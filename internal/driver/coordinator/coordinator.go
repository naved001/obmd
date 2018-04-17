// Package coordinator handles high-level console synchronization for drivers.
package coordinator

import (
	"context"
	"io"
	"log"
)

// A proc is a live "process" managing a console connection.
type Proc interface {
	// Shutdown disconnects the console session managed by this Proc.
	// If the session is already disconnected, this is a no-op.
	Shutdown() error

	// Reader returns an io.Reader that reads from the console.
	Reader() io.Reader
}

// A "primitive" OBM, from which the coordinator can build a driver.OBM.
type OBM interface {
	// Connect to the console, returning the managing Proc and an
	// error, if any.
	Dial() (Proc, error)
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

// An server manages console synchronization for a single OBM. It implements the
// console related methods of driver.OBM, and may be embedded in another struct
// which handles the non-console functionality.
//
// The zero value is not meaningful; use NewServer to create a value.
type Server struct {
	// Most of the server logic operates in it's own goroutine (see Serve).
	// The fields of this type are used by other goroutines to interact with
	// the server

	obm OBM

	// Requests to drop the console.
	dropConsole chan struct{}

	// Requests to connect to the console.
	dialConsole chan consoleReq

	// Requests to run a function atomically within the server.
	funcs chan func()
}

func (s *Server) Serve(ctx context.Context) {
	conn := &consoleConn{
		// This won't get used until we over-write `conn` with a
		// new connection, but we still need it to be non-nil to
		// have a receive case in the select statement below.
		drop: make(chan struct{}, 1),
	}

	var (
		proc Proc
		err  error
	)

	stopProcess := func() {
		if proc == nil {
			return
		}
		if err := proc.Shutdown(); err != nil {
			log.Println(
				"Error shutting down obm connection:",
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
			fn()
		case req := <-s.dialConsole:
			stopProcess()
			proc, err = s.obm.Dial()
			if err != nil {
				req.err <- err
				continue
			}
			conn = &consoleConn{
				// Buffer size of 1, so calls to Close() on the connection
				// don't block. Otherwise, if we've already dropped the
				// connection, Close() would deadlock.
				drop:   make(chan struct{}, 1),
				Reader: proc.Reader(),
			}
			req.conn <- conn
		}
	}
}

// Create a Server for the given OBM.
func NewServer(obm OBM) *Server {
	return &Server{
		obm:         obm,
		dropConsole: make(chan struct{}),
		dialConsole: make(chan consoleReq),
		funcs:       make(chan func()),
	}
}

// Disconnect the current console session. See driver.OBM.DropConsole.
func (s *Server) DropConsole() error {
	s.dropConsole <- struct{}{}
	return nil
}

// Connect to the console. This see driver.OBM.DialConsole
func (s *Server) DialConsole() (io.ReadCloser, error) {
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

// Run `fn` inside the server's main loop. This ensures that no (other) console
// related functionality is taken by the server while `fn` is running.
func (s *Server) RunInServer(fn func()) {
	done := make(chan struct{})
	s.funcs <- func() {
		fn()
		done <- struct{}{}
	}
	<-done
}
