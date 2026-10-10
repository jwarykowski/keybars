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

func TestField(t *testing.T) {
	// GetStatusReply{keymapp_version: "1.3", connected_keyboard: {friendly_name: "Moonlander MK1", current_layer: 2}}
	kb := append([]byte{0x0a, 14}, "Moonlander MK1"...)
	kb = append(kb, 0x18, 2)
	reply := append([]byte{0x0a, 3}, "1.3"...)
	reply = append(reply, 0x12, byte(len(kb)))
	reply = append(reply, kb...)
	got, ok := field(reply, 2)
	if !ok {
		t.Fatal("connected_keyboard not found")
	}
	name, ok := field(got, 1)
	if !ok || string(name) != "Moonlander MK1" {
		t.Errorf("friendly_name = %q, %v", name, ok)
	}
	if _, ok := field([]byte{0x0a, 3}, 1); ok {
		t.Error("truncated message: want not found")
	}
}
