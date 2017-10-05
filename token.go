package main

import (
	"bytes"
	"fmt"
)

// A cryptographically random 128-bit value.
type Token [128 / 8]byte

func (t Token) MarshalText() ([]byte, error) {
	return []byte(fmt.Sprintf("%0x", t)), nil
}

func (t *Token) UnmarshalText(text []byte) error {
	var buf []byte
	_, err := fmt.Fscanf(bytes.NewBuffer(text), "%32x", &buf)
	if err != nil {
		return err
	}
	copy(t[:], buf)
	return nil
}
