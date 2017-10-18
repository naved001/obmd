package mock

import (
	"encoding/json"
	"fmt"
	"io"
	"net"

	"github.com/zenhack/obmd/internal/driver"
	"github.com/zenhack/obmd/internal/driver/coordinator"
)

var Driver driver.Driver = mockDriver{}

// Mock driver for use in tests
type mockDriver struct{}

type mockInfo struct {
	Addr string `json:"addr"`
	User string `json:"user"`
	Pass string `json:"pass"`
}

type server struct {
	*coordinator.Server
	info mockInfo
}

type proc struct {
	conn net.Conn
}

func (p *proc) Shutdown() error {
	return p.conn.Close()
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

	go func() {
		var err error
		for err == nil {
			_, err = fmt.Fprintf(myConn, "%q:%q:%q\n", info.Addr, info.User, info.Pass)
		}
	}()

	return &proc{theirConn}, nil
}

func (*server) PowerOff() error             { panic("Not Implemented") }
func (*server) PowerCycle(force bool) error { panic("Not Implemented") }
func (*server) SetBootdev(dev string) error { panic("Not Implemented") }
