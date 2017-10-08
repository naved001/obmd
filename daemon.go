package main

import (
	"errors"
)

var (
	ErrNoSuchNode      = errors.New("No such node.")
	ErrInvalidToken    = errors.New("Invalid token.")
	ErrVersionConflict = errors.New("Version conflict")
)

type Daemon struct {
	state *State
	funcs chan func()
}

func (d *Daemon) runInDaemon(fn func()) {
	done := make(chan struct{})
	d.funcs <- func() {
		fn()
		done <- struct{}{}
	}
	<-done
}

func NewDaemon(state *State) *Daemon {
	ret := &Daemon{
		state: state,
		funcs: make(chan func()),
	}
	go func() {
		for {
			fn := <-ret.funcs
			fn()
		}
	}()
	return ret
}

func (d *Daemon) DeleteNode(label string) (err error) {
	d.runInDaemon(func() {
		err = d.state.DeleteNode(label)
	})
	return
}

func (d *Daemon) SetNode(label string, info []byte) (err error) {
	d.runInDaemon(func() {
		_, err = d.state.SetNode(label, info)
	})
	return
}

func (d *Daemon) GetNodeVersion(label string) (version uint64, err error) {
	d.runInDaemon(func() {
		var node *Node
		node, err = d.state.GetNode(label)
		if err != nil {
			// TODO: it would be better not to assume *any* error
			// from GetNode is a simple abscence.
			err = ErrNoSuchNode
			return
		}
		version = node.Version
	})
	return
}

/*
func (d *Daemon) SetNodeVersion(label string, version uint64) error
func (d *Daemon) GetNodeToken(label string, version uint64) (*Token, error)

func (d *Daemon) DialNodeConsole(label string, token *Token) (io.ReadCloser, error)
func (d *Daemon) PowerOffNode(label string, token *Token) error
func (d *Daemon) PowerCycleNode(label string, force bool, token *Token) error
func (d *Daemon) SetNodeBootDev(label string, dev string, token *Token) error
*/
