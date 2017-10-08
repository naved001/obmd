package main

import (
	"bytes"
	"crypto/rand"
	"fmt"
)

// A cryptographically random 128-bit value.
type Token [128 / 8]byte

// A dummy token to be used when there is no "valid" token. This is
// generated in init(), and never escapes the program. It exists so
// we don't have to have special purpose logic for the case where
// there is no correct token; we just set the node's token to this
// value which is inaccessable to *anyone*.
var noToken Token

func init() {
	_, err := rand.Read(noToken[:])
	chkfatal(err)
}

func (t Token) MarshalText() ([]byte, error) {
	return []byte(fmt.Sprintf("%0x", t)), nil
}

func (t *Token) UnmarshalText(text []byte) error {
	var buf []byte
	_, err := fmt.Fscanf(bytes.NewBuffer(text), "%32x", &buf)
	if err != nil {
		return fmt.Errorf("Error unmarshalling token: %v", err)
	}
	copy(t[:], buf)
	return nil
}
