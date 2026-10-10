package keymapp

import (
	"bytes"
	"testing"
)

func TestFields(t *testing.T) {
	// led 36, red 0 (omitted), green 200 (two-byte varint), blue 1
	got := fields(36, 0, 200, 1)
	want := []byte{0x08, 36, 0x18, 0xc8, 0x01, 0x20, 1}
	if !bytes.Equal(got, want) {
		t.Errorf("fields = % x, want % x", got, want)
	}
}
