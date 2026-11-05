package rgb

import (
	"errors"
	"testing"
)

type plain struct{}

func (plain) Set(int, Colour) error { return nil }
func (plain) RestoreAll() error     { return nil }

type buffered struct {
	plain
	flushes int
	err     error
}

func (b *buffered) Flush() error { b.flushes++; return b.err }

func TestFlush(t *testing.T) {
	broken := errors.New("broken")
	tests := []struct {
		name        string
		leds        LEDs
		wantFlushes int
		wantErr     error
	}{
		{"unbuffered does nothing", plain{}, 0, nil},
		{"buffered flushes", &buffered{}, 1, nil},
		{"flush error comes back", &buffered{err: broken}, 1, broken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Flush(tt.leds); !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
			if b, ok := tt.leds.(*buffered); ok && b.flushes != tt.wantFlushes {
				t.Errorf("flushes = %d, want %d", b.flushes, tt.wantFlushes)
			}
		})
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		in   string
		want Colour
		ok   bool
	}{
		{"#00e676", Colour{0, 0xe6, 0x76}, true},
		{"#FFB000", Colour{0xff, 0xb0, 0}, true},
		{"00e676", Colour{}, false},
		{"#fff", Colour{}, false},
		{"#gg0000", Colour{}, false},
		{"#+fffff", Colour{}, false},
		{"", Colour{}, false},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if got != tt.want || (err == nil) != tt.ok {
			t.Errorf("Parse(%q) = %v, %v", tt.in, got, err)
		}
	}
}
