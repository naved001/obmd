package main

import (
	"testing"
)

// tests of the Token type's methods

func TestUnmarshal(t *testing.T) {
	var actual Token
	err := (&actual).UnmarshalText([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("Error in unmarshal: %v", err)
	}
	expected := Token{
		0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
		0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
	}
	if actual != expected {
		t.Fatalf("TestUnmarshal: Expected %v but got %v", expected, actual)
	}
}
