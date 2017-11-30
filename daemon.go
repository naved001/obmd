package main

import (
	"errors"
	"io"
	"sync"
)

var (
	ErrNodeExists      = errors.New("Node already exists.")
	ErrNoSuchNode      = errors.New("No such node.")
	ErrInvalidToken    = errors.New("Invalid token.")
	ErrVersionConflict = errors.New("Version conflict")
)

type Daemon struct {
	sync.Mutex
	state *State
	funcs chan func()
}

func NewDaemon(state *State) *Daemon {
	return &Daemon{
		state: state,
	}
}

func (d *Daemon) DeleteNode(label string) error {
	d.Lock()
	defer d.Unlock()
	return d.state.DeleteNode(label)
}

func (d *Daemon) SetNode(label string, info []byte) error {
	d.Lock()
	defer d.Unlock()

	d.state.check()

	node, err := d.state.GetNode(label)
	if err == nil {
		// The node already exists; store the version, delete it, then
		// re-create it.
		version := node.Version
		if err = d.state.DeleteNode(label); err != nil {
			return err
		}
		// The node has been modified, so increment the version.
		_, err = d.state.NewNode(label, info, version+1)
	} else {
		// The node doesn't exist; just create it with version = 0
		_, err = d.state.NewNode(label, info, 0)
	}

	d.state.check()
	return err
}

func (d *Daemon) GetNodeVersion(label string) (version uint64, err error) {
	d.Lock()
	defer d.Unlock()
	d.state.check()
	node, err := d.state.GetNode(label)
	if err != nil {
		return 0, err
	}
	return node.Version, err
}

func (d *Daemon) SetNodeVersion(label string, version uint64) (newVersion uint64, err error) {
	d.Lock()
	defer d.Unlock()
	var node *Node
	node, err = d.state.GetNode(label)
	if err != nil {
		return 0, err
	}
	if version != node.Version+1 {
		return node.Version, ErrVersionConflict
	}
	err = d.state.BumpNodeVersion(label)
	if err == nil {
		node.OBM.DropConsole()
	}
	return node.Version, err
}

func (d *Daemon) GetNodeToken(label string, version uint64) (Token, uint64, error) {
	d.Lock()
	defer d.Unlock()
	node, err := d.state.GetNode(label)
	if err != nil {
		return Token{}, 0, err
	}
	if version != node.Version {
		return Token{}, node.Version, ErrVersionConflict
	}
	token, err := node.NewToken()
	if err != nil {
		return Token{}, node.Version, err
	}
	return token, node.Version, nil
}

func (d *Daemon) DialNodeConsole(label string, token *Token) (io.ReadCloser, error) {
	d.Lock()
	defer d.Unlock()
	node, err := d.state.GetNode(label)
	if err != nil {
		return nil, err
	}
	if !node.ValidToken(*token) {
		return nil, ErrInvalidToken
	}
	return node.OBM.DialConsole()
}

func (d *Daemon) PowerOffNode(label string, token *Token) error {
	d.Lock()
	defer d.Unlock()
	panic("Not implmeneted")
}

func (d *Daemon) PowerCycleNode(label string, force bool, token *Token) error {
	d.Lock()
	defer d.Unlock()
	panic("Not implmeneted")
}

func (d *Daemon) SetNodeBootDev(label string, dev string, token *Token) error {
	d.Lock()
	defer d.Unlock()
	panic("Not implmeneted")
}
