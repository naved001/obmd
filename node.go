package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"

	"github.com/zenhack/obmd/internal/driver"
)

// Information about a node
type Node struct {
	ConnInfo     []byte             // Connection info for this node's OBM.
	ObmCancel    context.CancelFunc // stop the OBM
	OBM          driver.OBM         // OBM for this node.
	CurrentToken Token              // Token for regular user operations.
}

// Returns a new node with the given driver information, with no valid token.
func NewNode(d driver.Driver, info []byte) (*Node, error) {
	obm, err := d.GetOBM(info)
	if err != nil {
		return nil, err
	}
	ret := &Node{
		OBM:      obm,
		ConnInfo: info,
	}
	copy(ret.CurrentToken[:], noToken[:])
	return ret, nil
}

// Generate a new token, invaidating the old one if any, and disconnecting
// clients using it. If an error occurs, the state of the node/token will
// be unchanged.
func (n *Node) NewToken() (Token, error) {
	var token Token
	_, err := rand.Read(token[:])
	if err != nil {
		return token, err
	}
	n.ClearToken()
	copy(n.CurrentToken[:], token[:])
	return n.CurrentToken, nil
}

// Return whether a token is valid.
func (n *Node) ValidToken(token Token) bool {
	return subtle.ConstantTimeCompare(n.CurrentToken[:], token[:]) == 1
}

// Clear any existing token, and disconnect any clients
func (n *Node) ClearToken() {
	n.OBM.DropConsole()
	copy(n.CurrentToken[:], noToken[:])
}

func (n *Node) StartOBM() {
	if n.ObmCancel != nil {
		panic("BUG: OBM is already started!")
	}
	ctx, cancel := context.WithCancel(context.Background())
	n.ObmCancel = cancel
	go n.OBM.Serve(ctx)
}

func (n *Node) StopOBM() {
	if n.ObmCancel == nil {
		panic("BUG: OBM is not running!")
	}
	n.ObmCancel()
	n.ObmCancel = nil
}
