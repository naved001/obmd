package main

import (
	"errors"
	"io"
	"sync"
)

var (
	ErrNodeExists   = errors.New("Node already exists.")
	ErrNoSuchNode   = errors.New("No such node.")
	ErrInvalidToken = errors.New("Invalid token.")
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

	_, err := d.state.GetNode(label)
	if err == nil {
		// The node already exists; delete it before (re)createing it.
		if err = d.state.DeleteNode(label); err != nil {
			return err
		}
	}
	// Create the node.
	_, err = d.state.NewNode(label, info)

	d.state.check()
	return err
}

func (d *Daemon) GetNodeToken(label string) (Token, error) {
	d.Lock()
	defer d.Unlock()
	node, err := d.state.GetNode(label)
	if err != nil {
		return Token{}, err
	}
	token, err := node.NewToken()
	if err != nil {
		return Token{}, err
	}
	return token, nil
}

func (d *Daemon) InvalidateNodeToken(label string) error {
	d.Lock()
	defer d.Unlock()
	node, err := d.state.GetNode(label)
	if err != nil {
		return err
	}
	node.ClearToken()
	return nil
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
