package openrgb

import (
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
)

// rogue accepts one client and answers its first packet with reply, then
// hangs up if hangUp.
func rogue(t *testing.T, reply []byte, hangUp bool) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var h [16]byte
		if _, err := io.ReadFull(conn, h[:]); err != nil {
			return
		}
		_, _ = io.CopyN(io.Discard, conn, int64(binary.LittleEndian.Uint32(h[12:])))
		_, _ = conn.Write(reply)
		if hangUp {
			return
		}
		_, _ = io.Copy(io.Discard, conn) // until the client hangs up
	}()
	return ln.Addr().String()
}

func TestDialRejectsBadServers(t *testing.T) {
	tests := []struct {
		name    string
		reply   []byte
		hangUp  bool
		wantErr string
	}{
		{"oversized packet", header(1, requestProtocolVersion, maxPacket+1), false, "too big"},
		{"bad header", []byte("XXXX0123456789ab"), false, "bad packet header"},
		{"hangs up mid-header", []byte("ORGB"), true, "EOF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Dial(rogue(t, tt.reply, tt.hangUp))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// matrixZone is a protocol 0 description of one matrix zone h by w, with
// the data for none of its cells.
func matrixZone(h, w uint32) []byte {
	b := make([]byte, 4)      // size, set below
	b = append(b, 0, 0, 0, 0) // type
	for range 5 {             // name, description, version, serial, location
		b = append(b, 0, 0)
	}
	b = append(b, 0, 0)       // no modes
	b = append(b, 0, 0, 0, 0) // active mode
	b = append(b, 1, 0)       // one zone
	b = append(b, 0, 0)       // its name
	b = append(b, 2, 0, 0, 0) // matrix type
	b = append(b, make([]byte, 12)...)
	b = append(b, 8, 0) // matrix length
	b = binary.LittleEndian.AppendUint32(b, h)
	b = binary.LittleEndian.AppendUint32(b, w)
	binary.LittleEndian.PutUint32(b, uint32(len(b)))
	return b
}

func TestParseControllerMatrixBounds(t *testing.T) {
	tests := []struct {
		name string
		h, w uint32
	}{
		{"past the data", 2, 2},
		{"overflows int", 0xFFFFFFFF, 0xFFFFFFFF},
		{"overflows when scaled", 0x80000000, 0x80000000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseController(matrixZone(tt.h, tt.w), 0)
			if err == nil || !strings.Contains(err.Error(), "runs past the data") {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func FuzzParseController(f *testing.F) {
	for _, name := range []string{"controller_v1.bin"} {
		b, err := readFixture(name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b, uint32(1))
	}
	f.Add(matrixZone(3, 3), uint32(0))
	f.Fuzz(func(_ *testing.T, b []byte, version uint32) {
		ctl, err := ParseController(b, version%2)
		if err == nil {
			ctl.Map()
		}
	})
}
