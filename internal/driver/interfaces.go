package driver

import (
	"context"
	"io"
)

// An OBM.
type OBM interface {
	// Manage the OBM. A goroutine executing Serve must be running when
	// any other OBM method.
	Serve(ctx context.Context)

	// Connect to the console. Returns the connection and any error.
	DialConsole() (io.ReadCloser, error)

	// Disconnect the current console session, if any.
	DropConsole() error

	// Power off the node.
	PowerOff() error

	// Reboot the node. `force` indicates whether to do a hard power off,
	// or a soft shutdown (giving the node's operating system a change to
	// respond).
	PowerCycle(force bool) error

	// Sets the next boot device to `dev`. Valid boot devices are
	// driver-dependent.
	SetBootdev(dev string) error

	// Gets the node's power status.
	GetPowerStatus() (string, error)
}

// A driver for a type of OBM.
type Driver interface {
	// Get an obm object based on the provided info.
	GetOBM(info []byte) (OBM, error)
}
