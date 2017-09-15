package main

import (
	"fmt"
	"io"
	"log"
	"net"
)

// A "dummy" IpmiDialer, that rather than actually speaking Ipmi,
// Connects to the Addr it is passed via tcp, sends the info to
// the destination address, and then returns that connection.
// This is useful for experimentation.
type DummyIpmiDialer struct {
}

func (d *DummyIpmiDialer) DialIpmi(info *IpmiInfo) (io.ReadCloser, error) {
	conn, err := net.Dial("tcp", info.Addr)
	if err != nil {
		return nil, err
	}
	_, err = fmt.Fprintln(conn, info)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func (d *DummyIpmiDialer) PowerOff(info *IpmiInfo) error {
	log.Println("Powering off:", info)
	return nil
}

func (d *DummyIpmiDialer) PowerCycle(info *IpmiInfo, force bool) error {
	log.Printf("Powering off: %v (force = %v)\n", info, force)
	return nil
}

func (d *DummyIpmiDialer) SetBootdev(info *IpmiInfo, dev string) error {
	log.Printf("Setting bootdev = %v: %v\n", dev, info)
	return nil
}
