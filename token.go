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
	if len(text) != 2*len(t[:]) {
		// wrong number of characters.
		return ErrInvalidToken
	}
	for _, char := range text {
		if !isHexDigit(char) {
			return ErrInvalidToken
		}
	}
	var buf []byte
	_, err := fmt.Fscanf(bytes.NewBuffer(text), "%32x", &buf)
	if err != nil {
		return ErrInvalidToken
	}
	copy(t[:], buf)
	return nil
}

func isHexDigit(char byte) bool {
	return char >= '0' && char <= '9' ||
		char >= 'a' && char <= 'f' ||
		char >= 'A' && char <= 'F'
}
