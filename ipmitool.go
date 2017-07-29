package main

import (
	"io"
	"os"
	"os/exec"

	"github.com/kr/pty"
)

type IpmitoolDialer struct {
}

// A running ipmi process, connected to a serial console. It's Close() method
// kills the process as well as closing it's attached pty.
type ipmiProcess struct {
	io.ReadWriteCloser
	proc *os.Process
}

func (p *ipmiProcess) Close() error {
	p.proc.Kill()
	p.proc.Wait()
	return p.ReadWriteCloser.Close()
}

func (d *IpmitoolDialer) DialIpmi(info *IpmiInfo) (io.ReadWriteCloser, error) {
	cmd := exec.Command(
		"ipmitool",
		"-I", "lanplus",
		"-U", info.User,
		"-P", info.Pass,
		"-H", info.Addr,
		"sol", "activate",
	)
	stdio, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	return &ipmiProcess{
		ReadWriteCloser: stdio,
		proc:            cmd.Process,
	}, nil
}
