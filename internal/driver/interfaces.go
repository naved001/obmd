package driver

import (
	"context"
	"io"
)

type OBM interface {
	DialConsole() (io.ReadCloser, error)
	DropConsole() error
	PowerOff() error
	PowerCycle(force bool) error
	SetBootdev(dev string) error
	Serve(ctx context.Context)
}

type Driver interface {
	GetOBM(info []byte) (OBM, error)
}
