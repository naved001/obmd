// Package mock implements a mock driver for testing purposes.
//
// Valid boot devices are "A" and "B".
package mock

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/CCI-MOC/obmd/internal/driver"
	"github.com/CCI-MOC/obmd/internal/driver/coordinator"
)

var Driver driver.Driver = mockDriver{}

type PowerAction string

const (
	Off         PowerAction = "off"
	ForceReboot             = "force-reboot"
	SoftReboot              = "soft-reboot"
	BootDevA                = "bootdev-a"
	BootDevB                = "bootdev-b"
)

var (
	// A mapping from node addrs (the "addr" field in the obm info) to the last power action
	// that was preformed on the OBM.
	LastPowerActions     = map[string]PowerAction{}
	lastPowerActionsLock sync.Mutex
)

// Mock driver for use in tests
type mockDriver struct{}

type mockInfo struct {
	Addr      string `json:"addr"`
	NumWrites int
}

type server struct {
	*coordinator.Server
	info mockInfo
}

type proc struct {
	done chan struct{}
	conn net.Conn
}

func (p *proc) Shutdown() error {
	err := p.conn.Close()
	<-p.done
	p.done = nil
	return err
}

func (p *proc) Reader() io.Reader {
	return p.conn
}

func (mockDriver) GetOBM(info []byte) (driver.OBM, error) {
	ret := &server{}
	err := json.Unmarshal(info, &ret.info)
	if err != nil {
		return nil, err
	}
	ret.Server = coordinator.NewServer(&ret.info)
	return ret, nil
}

// Connect to a mock console stream. It just writes "addr":"user":"pass" in a
// loop until the connection is closed.
func (info *mockInfo) Dial() (coordinator.Proc, error) {
	myConn, theirConn := net.Pipe()

	done := make(chan struct{})

	go func() {
		var err error
		for err == nil {
			_, err = fmt.Fprintf(myConn, "%d\n", info.NumWrites)
			info.NumWrites++
		}
		done <- struct{}{}
	}()

	return &proc{
		done: done,
		conn: theirConn,
	}, nil
}

func (s *server) setPowerAction(action PowerAction) {
	lastPowerActionsLock.Lock()
	defer lastPowerActionsLock.Unlock()
	LastPowerActions[s.info.Addr] = action
}

func (s *server) GetPowerStatus() (string, error) {
	return "Mock Status", nil
}

func (s *server) PowerOff() error {
	s.setPowerAction(Off)
	return nil
}
func (s *server) PowerCycle(force bool) error {
	if force {
		s.setPowerAction(ForceReboot)
		return nil
	} else {
		s.setPowerAction(SoftReboot)
		return nil
	}
}

func (s *server) SetBootdev(dev string) error {
	switch dev {
	case "A":
		s.setPowerAction(BootDevA)
		return nil
	case "B":
		s.setPowerAction(BootDevB)
		return nil
	}
	return driver.ErrInvalidBootdev
}
