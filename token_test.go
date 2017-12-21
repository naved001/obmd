package main

import (
	"testing"
)

// tests of the Token type's methods

// Test successful case for Token.UnmarshalText.
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

// Check UnmarshalText that invalid tokens return an ErrInvalidToken
func TestUnmarshalInvalid(t *testing.T) {
	cases := []string{
		// Too short:
		"1234",
		"",
		"abcdef0123456",

		// Too long:
		"0123456789abcdef0123456789abcdef1",

		// Odd numbers of digits. this is interesting because it would
		// imply partial bytes:
		"212",
		"1",

		// Invalid characters:
		"0123456789$#cdef0123456789abcdef",
	}

	var token Token
	for i, v := range cases {
		err := (&token).UnmarshalText([]byte(v))
		switch err {
		case ErrInvalidToken:
			// Correct.
		case nil:
			t.Errorf("Unexpected success unmarshalling %q in test case %v", v, i)
		default:
			t.Errorf("Incorrect error unmarshalling %q in test case %v: %v", v, i, err)
		}
	}
}
