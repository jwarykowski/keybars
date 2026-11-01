package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeOpenRGB is an OpenRGB SDK server at protocol 1 with the devices descs.
// It answers what keybars asks, and counts the frames (UPDATELEDS) it's sent.
func fakeOpenRGB(t *testing.T, descs ...[]byte) (string, *atomic.Int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var frames atomic.Int32
	serve := func(conn net.Conn) {
		defer conn.Close()
		reply := func(dev, id uint32, data []byte) {
			h := []byte("ORGB")
			h = binary.LittleEndian.AppendUint32(h, dev)
			h = binary.LittleEndian.AppendUint32(h, id)
			h = binary.LittleEndian.AppendUint32(h, uint32(len(data)))
			_, _ = conn.Write(append(h, data...)) // keybars reports a short reply
		}
		for {
			var h [16]byte
			if _, err := io.ReadFull(conn, h[:]); err != nil {
				return
			}
			dev, id := binary.LittleEndian.Uint32(h[4:]), binary.LittleEndian.Uint32(h[8:])
			if _, err := io.CopyN(io.Discard, conn, int64(binary.LittleEndian.Uint32(h[12:]))); err != nil {
				return
			}
			switch id {
			case 40: // protocol version
				reply(0, id, binary.LittleEndian.AppendUint32(nil, 1))
			case 0: // device count
				reply(0, id, binary.LittleEndian.AppendUint32(nil, uint32(len(descs))))
			case 1: // device description
				reply(dev, id, descs[dev])
			case 1050:
				frames.Add(1)
			}
		}
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serve(conn)
		}
	}()
	return ln.Addr().String(), &frames
}

// testKeyboard is the OpenRGB package's test device: 11 LEDs, a 4x3 matrix.
func testKeyboard(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("pkg", "openrgb", "testdata", "controller_v1.bin"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// noMatrix describes a per-LED keyboard called name with n LEDs and no key
// matrix, at protocol 1.
func noMatrix(name string, n int) []byte {
	le := binary.LittleEndian
	str := func(b []byte, s string) []byte {
		b = le.AppendUint16(b, uint16(len(s)+1))
		return append(append(b, s...), 0)
	}
	b := make([]byte, 4)      // size, set below
	b = le.AppendUint32(b, 5) // a keyboard
	for _, s := range []string{name, "vendor", "description", "version", "serial", "location"} {
		b = str(b, s)
	}
	b = le.AppendUint16(b, 1) // one mode
	b = le.AppendUint32(b, 0) // active
	b = str(b, "Direct")
	b = le.AppendUint32(b, 0)          // value
	b = le.AppendUint32(b, 1<<5)       // per-LED
	b = append(b, make([]byte, 24)...) // speeds, colour counts, direction
	b = le.AppendUint32(b, 1)          // per-LED colours
	b = le.AppendUint16(b, 0)          // no mode colours
	b = le.AppendUint16(b, 0)          // no zones
	b = le.AppendUint16(b, uint16(n))
	for i := range n {
		b = le.AppendUint32(str(b, fmt.Sprintf("Key %d", i)), 0)
	}
	b = le.AppendUint16(b, uint16(n))
	b = append(b, make([]byte, 4*n)...)
	le.PutUint32(b, uint32(len(b)))
	return b
}

func TestListOpenRGB(t *testing.T) {
	tests := []struct {
		name string
		desc []byte
		want []string
	}{
		{"with a key matrix", testKeyboard(t), []string{"0   Test Keyboard: 11 LEDs, 4x3 key matrix", "OpenRGB's LED index for each key"}},
		{"without one", noMatrix("Mystery 9000", 10), []string{"Mystery 9000: 10 LEDs, no key matrix", "gives no key matrix"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, _ := fakeOpenRGB(t, tt.desc)
			out, err := stdout(t, func() error { return listMaps([]string{"-backend", "openrgb", "-openrgb", addr}) })
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("output lacks %q:\n%s", w, out)
				}
			}
		})
	}
}

func TestOpenRGBMaps(t *testing.T) {
	tests := []struct {
		name    string
		desc    []byte
		args    []string
		wantErr string
	}{
		{"the device's key matrix", testKeyboard(t), nil, ""},
		{"QMK's map for a board OpenRGB gives none for", noMatrix("Corne", 60), nil, ""},
		{"no map at all", noMatrix("Mystery 9000", 10), nil, "OpenRGB gives no key matrix for Mystery 9000"},
		{"a Keymapp model", testKeyboard(t), []string{"-model", "planck"}, "-model maps are Keymapp's numbering"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, frames := fakeOpenRGB(t, tt.desc)
			args := append([]string{"-backend", "openrgb", "-openrgb", addr, "-step", "1ms"}, tt.args...)
			_, err := stdout(t, func() error { return test(args) })
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if frames.Load() == 0 {
				t.Error("no frames sent")
			}
		})
	}
}

func TestRunOpenRGB(t *testing.T) {
	addr, frames := fakeOpenRGB(t, testKeyboard(t))
	err := withStdin(t, "1;2;0;1\n", func() error {
		return run([]string{"-backend", "openrgb", "-openrgb", addr, "-stdin", "-idle", "1h"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if frames.Load() == 0 {
		t.Error("no frames sent")
	}
}

func TestFindOpenRGB(t *testing.T) {
	addr, _ := fakeOpenRGB(t, testKeyboard(t))
	out, err := stdout(t, func() error {
		return find([]string{"-backend", "openrgb", "-openrgb", addr, "-auto", "1ms"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"LED 0 (Key: Esc)", "LED 10 (Logo)", "no LED 11: done"} {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
}
