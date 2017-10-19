package main

import (
	"errors"
	"io"
	"sync"
)

var (
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

func (d *Daemon) SetNode(label string, info []byte) (err error) {
	d.Lock()
	defer d.Unlock()
	_, err = d.state.SetNode(label, info)
	return
}

func (d *Daemon) GetNodeVersion(label string) (version uint64, err error) {
	d.Lock()
	defer d.Unlock()
	var node *Node
	node, err = d.state.GetNode(label)
	if err != nil {
		// TODO: it would be better not to assume *any* error
		// from GetNode is a simple abscence.
		err = ErrNoSuchNode
		return
	}
	version = node.Version
	return
}

func (d *Daemon) SetNodeVersion(label string, version uint64) (newVersion uint64, err error) {
	d.Lock()
	defer d.Unlock()
	var node *Node
	node, err = d.state.GetNode(label)
	if err != nil {
		err = ErrNoSuchNode
		return
	}
	oldVersion := node.Version
	if version != oldVersion+1 {
		return oldVersion, ErrVersionConflict
	}
	// XXX: Slightly gross: SetNode bumps the version number itself, so we
	// don't have to actually pass in the new version, but it would be nice
	// if the check above didn't have to be coordinated separately.
	node, err = d.state.SetNode(label, node.ConnInfo)
	if err != nil {
		newVersion = oldVersion
		return
	}
	newVersion = node.Version
	return
}

func (d *Daemon) GetNodeToken(label string, version uint64) (*Token, uint64, error) {
	d.Lock()
	defer d.Unlock()
	node, err := d.state.GetNode(label)
	if err != nil {
		return nil, 0, err
	}
	token, err := node.NewToken()
	if err != nil {
		return nil, node.Version, err
	}
	if version != node.Version {
		return nil, node.Version, ErrVersionConflict
	}
	return &token, node.Version, nil
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
