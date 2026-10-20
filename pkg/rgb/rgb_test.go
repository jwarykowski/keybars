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
