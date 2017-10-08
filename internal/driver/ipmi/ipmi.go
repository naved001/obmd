// Package ipmi implements an OBM driver for IPMI controllers.
package ipmi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"syscall"

	"github.com/kr/pty"

	"github.com/zenhack/obmd/internal/driver"
)

var Driver driver.Driver = impiDriver{}

type impiDriver struct{}

func (impiDriver) GetOBM(info []byte) (driver.OBM, error) {
	var connInfo connInfo
	err := json.Unmarshal(info, &connInfo)
	if err != nil {
		return nil, err
	}
	return newServer(&connInfo), nil
}

// connInfo contains the connection info for an IPMI controller.
type connInfo struct {
	Addr string `json:"addr"`
	User string `json:"user"`
	Pass string `json:"pass"`
}

// A running ipmi process, connected to a serial console. Its Close() method:
//
// * kills the process
// * cleans up the ipmi controller's sol state
// * closes its attached pty
type ipmitoolProcess struct {
	info *connInfo
	proc *os.Process
	r    io.ReadCloser
}

// A request to connect to the console. If the request succeeds, the connection
// is sent on `conn`. Otherwise, an error is sent on `err`.
type consoleReq struct {
	err  chan error
	conn chan io.ReadCloser
}

// A connection to a console.
type consoleConn struct {
	drop    chan struct{}
	dropped bool
	io.Reader
}

func (c *consoleConn) Close() error {
	if !c.dropped {
		c.dropped = true
		c.drop <- struct{}{}
	}
	return nil
}

// An server manages a single ipmi controller.
type server struct {
	// Most of the server logic operates in it's own goroutine (see ServeIpmi).
	// The fields of this type are used by other goroutines to interact with
	// the server

	// Requests to drop the console.
	dropConsole chan struct{}

	// Requests to connect to the console.
	dialConsole chan consoleReq

	// Requests to run a function atomically, with the connection info for
	// the controller.
	funcs chan func(*connInfo)

	// The conection info for this ipmi controller.
	connInfo *connInfo
}

// Cleanly disconnect from the console.
//
// It is safe to call this method on a nil pointer; in this case the method
// is a no-op.
//
// This kills the running ipmitool process, and then makes a call to
// deactivate sol.
func (p *ipmitoolProcess) shutdown() error {
	if p == nil {
		return nil
	}
	p.proc.Signal(syscall.SIGTERM)
	p.proc.Wait()
	errDeactivate := ipmitool(p.info, "sol", "deactivate").Run()
	errClose := p.r.Close()
	if errDeactivate != nil {
		return errDeactivate
	}
	return errClose
}

func (s *server) Serve(ctx context.Context) {
	conn := &consoleConn{
		// This won't get used until we over-write `conn` with a
		// new connection, but we still need it to be non-nil to
		// have a receive case in the select statement below.
		drop: make(chan struct{}, 1),
	}

	var proc *ipmitoolProcess

	stopProcess := func() {
		if err := proc.shutdown(); err != nil {
			log.Println(
				"Error shutting down ipmitool process:",
				err, "continuing, but this could potentially",
				"cause problems.",
			)
		}
		proc = nil
	}

	for {
		select {
		case <-ctx.Done():
			stopProcess()
			return
		case <-conn.drop:
			stopProcess()
		case <-s.dropConsole:
			stopProcess()
		case fn := <-s.funcs:
			fn(s.connInfo)
		case req := <-s.dialConsole:
			stopProcess()
			proc, err := dialConsole(s.connInfo)
			if err != nil {
				req.err <- err
				continue
			}
			conn = &consoleConn{
				// Buffer size of 1, so calls to Close() on the connection
				// don't block. Otherwise, if we've already dropped the
				// connection, Close() would deadlock.
				drop:   make(chan struct{}, 1),
				Reader: proc.r,
			}
			req.conn <- conn
		}
	}
}

// Launch an IpmiServer for the controller indicated by info.
func newServer(info *connInfo) *server {
	return &server{
		dropConsole: make(chan struct{}),
		dialConsole: make(chan consoleReq),
		funcs:       make(chan func(*connInfo)),
		connInfo:    info,
	}
}

var (
	ErrInvalidBootdev = errors.New("Invalid boot device.")
)

func (s *server) DropConsole() error {
	s.dropConsole <- struct{}{}
	return nil
}

func (s *server) DialConsole() (io.ReadCloser, error) {
	req := consoleReq{
		err:  make(chan error),
		conn: make(chan io.ReadCloser),
	}
	s.dialConsole <- req
	select {
	case err := <-req.err:
		return nil, err
	case conn := <-req.conn:
		return conn, nil
	}
}

// Connect to the serial console for the impi controller specified by info.
// If the error is non nil, the process will be nil.
func dialConsole(info *connInfo) (*ipmitoolProcess, error) {
	cmd := ipmitool(info, "sol", "activate")
	stdio, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	return &ipmitoolProcess{
		r:    stdio,
		proc: cmd.Process,
		info: info,
	}, nil
}

// Run `fn` inside the server's main loop, with the server's connection
// info.
func (s *server) runInServer(fn func(*connInfo)) {
	done := make(chan struct{})
	s.funcs <- func(info *connInfo) {
		fn(info)
		done <- struct{}{}
	}
	<-done
}

// Invoke ipmitool, adding the connection parameters for the server's
// controller.
func (s *server) ipmitool(args ...string) (err error) {
	s.runInServer(func(info *connInfo) {
		err = ipmitool(info, args...).Run()
	})
	return
}

// Invoke ipmitool, adding connection parameters corresponding to `info`.
func ipmitool(info *connInfo, args ...string) *exec.Cmd {
	// Annoyingly, when invoking a variadic function f(x ...Foo), you can't
	// just do Foo(x, y, z, ...more); you need either Foo(x, y, z) or
	// Foo(...more). We work around this by adding the static arguments to
	// the slice, and then doing the latter:
	args = append([]string{
		"-I", "lanplus",
		"-U", info.User,
		"-P", info.Pass,
		"-H", info.Addr,
	}, args...)
	return exec.Command("ipmitool", args...)
}

// Power off the server.
func (s *server) PowerOff() error {
	return s.ipmitool("chassis", "power", "off")
}

// Reboot the server. `force` indicates whether to do a forced shutdown, or
// to give the operating system a chance to respond.
func (s *server) PowerCycle(force bool) (err error) {
	var op string
	if force {
		op = "reset"
	} else {
		op = "cycle"
	}
	s.runInServer(func(info *connInfo) {
		err = ipmitool(info, "chassis", "power", op).Run()
		if err == nil {
			return
		}
		// The above can fail if the machine is already powered off; in
		// this case we just turn it on:
		err = ipmitool(info, "chassis", "power", "on").Run()
	})
	return
}

// Set the boot device. Legal values are "disk", "pxe", and "none".
// TODO: document what `none` indicates.
func (s *server) SetBootdev(dev string) error {
	if dev != "disk" && dev != "pxe" && dev != "none" {
		return ErrInvalidBootdev
	}
	return s.ipmitool("chassis", "bootdev", dev, "options=persistent")
}
