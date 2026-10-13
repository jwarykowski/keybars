// Package keymapp talks to Keymapp's gRPC API (the one kontroll uses) over
// its unix socket, holding one connection open so LED updates cost about a
// millisecond each instead of a process start.
//
// Requests are hand-encoded protobuf: the few messages used here are flat
// int32 fields, so no generated code is needed.
package keymapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/jwarykowski/keybars/pkg/rgb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Colour is an RGB colour.
type Colour = rgb.Colour

// LEDs is what the renderer needs from a keyboard.
type LEDs = rgb.LEDs

// Client is a connection to Keymapp.
type Client struct {
	conn *grpc.ClientConn
}

// SocketPath finds Keymapp's socket as kontroll does: $KEYMAPP_SOCKET, else
// .keymapp/keymapp.sock in the user config dir, else (macOS App Store build)
// inside its sandbox container.
func SocketPath() (string, error) {
	if p := os.Getenv("KEYMAPP_SOCKET"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, ".keymapp", "keymapp.sock")
	if _, err := os.Stat(p); err == nil || runtime.GOOS != "darwin" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library/Containers/io.zsa.keymapp/Data/Library/Application Support/.keymapp/keymapp.sock"), nil
}

// Dial connects to the socket at path.
func Dial(path string) (*Client, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("keymapp socket %s: %w (is Keymapp running with its API on?)", path, err)
	}
	conn, err := grpc.NewClient("unix://"+path,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(raw{})))
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn}, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Set colours one LED until changed.
func (c *Client) Set(led int, col Colour) error {
	return c.call("SetRGBLed", fields(led, int(col.R), int(col.G), int(col.B), 0))
}

// RestoreAll hands the board back to the layout's colours. Keymapp does it
// by ending API control, so it is all or nothing: there is no per-key
// restore, and while the API drives any key the rest are dark.
func (c *Client) RestoreAll() error {
	return c.call("SetRGBAll", fields(0, 0, 0, 1))
}

// SetAll colours every LED until changed.
func (c *Client) SetAll(col Colour) error {
	return c.call("SetRGBAll", fields(int(col.R), int(col.G), int(col.B), 0))
}

// Keyboard is the connected keyboard's name, e.g. "Moonlander MK1".
func (c *Client) Keyboard() (string, error) {
	reply, err := c.invoke("GetStatus", nil)
	if err != nil {
		return "", err
	}
	// GetStatusReply{2: ConnectedKeyboard{1: friendly_name}}
	kb, ok := field(reply, 2)
	if !ok {
		return "", errors.New("no keyboard connected to Keymapp")
	}
	name, _ := field(kb, 1)
	return string(name), nil
}

// call invokes a method whose reply is {bool success = 1}.
func (c *Client) call(method string, req []byte) error {
	reply, err := c.invoke(method, req)
	if err != nil {
		return err
	}
	if len(reply) < 2 || reply[0] != 0x08 || reply[1] != 1 {
		return errors.New(method + ": keymapp reported failure (is a keyboard connected?)")
	}
	return nil
}

// invoke calls a KeyboardService method and returns the encoded reply.
func (c *Client) invoke(method string, req []byte) ([]byte, error) {
	var reply []byte
	err := c.conn.Invoke(context.Background(), "/api.KeyboardService/"+method, &req, &reply)
	return reply, err
}

// field returns the bytes of length-delimited field n in an encoded message,
// skipping varint fields; enough for the string and message fields read here.
func field(b []byte, n int) ([]byte, bool) {
	for len(b) > 0 {
		tag, k := uvarint(b)
		if k == 0 {
			return nil, false
		}
		b = b[k:]
		switch tag & 7 {
		case 0: // varint
			_, k = uvarint(b)
			if k == 0 {
				return nil, false
			}
			b = b[k:]
		case 2: // length-delimited
			l, k := uvarint(b)
			if k == 0 || uint64(len(b)-k) < l {
				return nil, false
			}
			v := b[k : k+int(l)]
			if int(tag>>3) == n {
				return v, true
			}
			b = b[k+int(l):]
		default:
			return nil, false
		}
	}
	return nil, false
}

// uvarint decodes a varint, returning it and the bytes read (0 on error).
func uvarint(b []byte) (uint64, int) {
	var v uint64
	for i := 0; i < len(b) && i < 10; i++ {
		v |= uint64(b[i]&0x7f) << (7 * i)
		if b[i] < 0x80 {
			return v, i + 1
		}
	}
	return 0, 0
}

// fields encodes int32 fields 1..n as protobuf varints, skipping zeros as
// proto3 does.
func fields(vs ...int) []byte {
	var b []byte
	for i, v := range vs {
		if v == 0 {
			continue
		}
		b = append(b, byte((i+1)<<3))
		for v >= 0x80 {
			b = append(b, byte(v)|0x80)
			v >>= 7
		}
		b = append(b, byte(v))
	}
	return b
}

// raw passes pre-encoded protobuf bytes through gRPC.
type raw struct{}

func (raw) Marshal(v any) ([]byte, error)   { return *(v.(*[]byte)), nil }
func (raw) Unmarshal(b []byte, v any) error { *(v.(*[]byte)) = append([]byte(nil), b...); return nil }
func (raw) Name() string                    { return "proto" }
