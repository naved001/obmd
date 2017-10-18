// Package ipmi implements an OBM driver for IPMI controllers.
package ipmi

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/kr/pty"

	"github.com/zenhack/obmd/internal/driver"
	"github.com/zenhack/obmd/internal/driver/coordinator"
)

var Driver driver.Driver = impiDriver{}

type impiDriver struct{}

func (impiDriver) GetOBM(info []byte) (driver.OBM, error) {
	connInfo := &connInfo{}
	err := json.Unmarshal(info, connInfo)
	if err != nil {
		return nil, err
	}
	return &server{
		Server: coordinator.NewServer(connInfo),
		info:   connInfo,
	}, nil
}

// connInfo contains the connection info for an IPMI controller.
type connInfo struct {
	Addr string `json:"addr"`
	User string `json:"user"`
	Pass string `json:"pass"`
}

// A running ipmi process, connected to a serial console. Its Shutdown() method:
//
// * kills the process
// * cleans up the ipmi controller's sol state
// * closes its attached pty
type ipmitoolProcess struct {
	info *connInfo
	proc *os.Process
	r    io.ReadCloser
}

// An server manages a single ipmi controller.
type server struct {
	*coordinator.Server
	info *connInfo
}

// Cleanly disconnect from the console.
//
// This kills the running ipmitool process, and then makes a call to
// deactivate sol.
func (p *ipmitoolProcess) Shutdown() error {
	p.proc.Signal(syscall.SIGTERM)
	p.proc.Wait()
	errDeactivate := p.info.ipmitool("sol", "deactivate").Run()
	errClose := p.r.Close()
	if errDeactivate != nil {
		return errDeactivate
	}
	return errClose
}

func (p *ipmitoolProcess) Reader() io.Reader {
	return p.r
}

func (info *connInfo) Dial() (coordinator.Proc, error) {
	cmd := info.ipmitool("sol", "activate")
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

// Invoke ipmitool, adding connection parameters corresponding to `info`.
func (info *connInfo) ipmitool(args ...string) *exec.Cmd {
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

// Invoke ipmitool in the server's main loop, passing extra arguments
// with the connection info for this ipmi controller.
func (s *server) ipmitool(args ...string) (err error) {
	s.RunInServer(func() {
		err = s.info.ipmitool(args...).Run()
	})
	return
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
	s.RunInServer(func() {
		err = s.info.ipmitool("chassis", "power", op).Run()
		if err == nil {
			return
		}
		// The above can fail if the machine is already powered off; in
		// this case we just turn it on:
		err = s.info.ipmitool("chassis", "power", "on").Run()
	})
	return
}

// Set the boot device. Legal values are "disk", "pxe", and "none".
// TODO: document what `none` indicates.
func (s *server) SetBootdev(dev string) error {
	if dev != "disk" && dev != "pxe" && dev != "none" {
		return driver.ErrInvalidBootdev
	}
	return s.ipmitool("chassis", "bootdev", dev, "options=persistent")
}
