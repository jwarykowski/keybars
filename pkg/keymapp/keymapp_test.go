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

func TestFieldMalformed(t *testing.T) {
	name := append([]byte{0x0a, 3}, "abc"...)
	tests := []struct {
		name   string
		msg    []byte
		n      int
		want   []byte
		wantOK bool
	}{
		{"skips varints", append([]byte{0x08, 0x96, 0x01}, name...), 1, []byte("abc"), true},
		{"empty value", []byte{0x0a, 0}, 1, []byte{}, true},
		{"absent", name, 2, nil, false},
		{"empty message", nil, 1, nil, false},
		{"length past the end", []byte{0x0a, 9, 'a'}, 1, nil, false},
		{"huge length", []byte{0x0a, 0xff, 0xff, 0xff, 0xff, 0x0f}, 1, nil, false},
		{"truncated varint", []byte{0x08, 0x80}, 1, nil, false},
		{"unterminated tag", []byte{0x80, 0x80}, 1, nil, false},
		{"fixed64 unsupported", []byte{0x09, 0, 0, 0, 0, 0, 0, 0, 0}, 1, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := field(tt.msg, tt.n)
			if ok != tt.wantOK || !bytes.Equal(got, tt.want) {
				t.Errorf("field(% x, %d) = %q, %v; want %q, %v", tt.msg, tt.n, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func FuzzField(f *testing.F) {
	f.Add([]byte{0x0a, 3, 'a', 'b', 'c'}, 1)
	f.Add([]byte{0x08, 0x96, 0x01, 0x12, 0}, 2)
	f.Fuzz(func(t *testing.T, b []byte, n int) {
		if v, ok := field(b, n); ok && len(v) > len(b) {
			t.Errorf("field returned %d bytes from %d", len(v), len(b))
		}
	})
}
