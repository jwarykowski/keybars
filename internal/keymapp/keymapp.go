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

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Colour is an RGB colour.
type Colour struct{ R, G, B uint8 }

// LEDs is what the renderer needs from a keyboard, so tests can record calls.
type LEDs interface {
	// Set colours one LED.
	Set(led int, c Colour) error
	// Restore returns one LED to its layout colour.
	Restore(led int) error
	// RestoreAll returns every LED to its layout colour.
	RestoreAll() error
}

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

// Restore returns one LED to its layout colour: Keymapp reverts a colour
// once its sustain (ms) runs out.
func (c *Client) Restore(led int) error {
	return c.call("SetRGBLed", fields(led, 0, 0, 0, 1))
}

// RestoreAll returns every LED to its layout colour.
func (c *Client) RestoreAll() error {
	return c.call("SetRGBAll", fields(0, 0, 0, 1))
}

// call invokes a KeyboardService method; every reply is {bool success = 1}.
func (c *Client) call(method string, req []byte) error {
	var reply []byte
	if err := c.conn.Invoke(context.Background(), "/api.KeyboardService/"+method, &req, &reply); err != nil {
		return err
	}
	if len(reply) < 2 || reply[0] != 0x08 || reply[1] != 1 {
		return errors.New(method + ": keymapp reported failure (is a keyboard connected?)")
	}
	return nil
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
