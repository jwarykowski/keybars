package keymapp

import (
	"bytes"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/jwarykowski/keybars/pkg/rgb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// call is a request the fake Keymapp got.
type call struct {
	method string
	req    []byte
}

// fakeKeymapp answers KeyboardService calls on a unix socket with replies,
// by method; a method missing from replies fails as Unimplemented.
type fakeKeymapp struct {
	mu    sync.Mutex
	calls []call
}

func serve(t *testing.T, replies map[string][]byte) (string, *fakeKeymapp) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Keymapp's socket is a unix socket")
	}
	f := &fakeKeymapp{}
	path := filepath.Join(t.TempDir(), "k.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer(grpc.ForceServerCodec(raw{}), grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		full, _ := grpc.MethodFromServerStream(stream)
		method := strings.TrimPrefix(full, "/api.KeyboardService/")
		var req []byte
		if err := stream.RecvMsg(&req); err != nil {
			return err
		}
		f.mu.Lock()
		f.calls = append(f.calls, call{method, req})
		f.mu.Unlock()
		reply, ok := replies[method]
		if !ok {
			return status.Error(codes.Unimplemented, method)
		}
		return stream.SendMsg(&reply)
	}))
	go func() { _ = s.Serve(l) }()
	t.Cleanup(s.Stop)
	return path, f
}

func (f *fakeKeymapp) got() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

var success = []byte{0x08, 1}

func TestClientCalls(t *testing.T) {
	tests := []struct {
		name    string
		do      func(c *Client) error
		replies map[string][]byte
		want    call
		wantErr string
	}{
		{
			name:    "Set sends the LED and colour",
			do:      func(c *Client) error { return c.Set(36, rgb.Colour{R: 255, B: 1}) },
			replies: map[string][]byte{"SetRGBLed": success},
			want:    call{"SetRGBLed", fields(36, 255, 0, 1, 0)},
		},
		{
			name:    "SetAll sends the colour",
			do:      func(c *Client) error { return c.SetAll(rgb.Colour{G: 9}) },
			replies: map[string][]byte{"SetRGBAll": success},
			want:    call{"SetRGBAll", fields(0, 9, 0, 0)},
		},
		{
			name:    "RestoreAll ends API control",
			do:      func(c *Client) error { return c.RestoreAll() },
			replies: map[string][]byte{"SetRGBAll": success},
			want:    call{"SetRGBAll", fields(0, 0, 0, 1)},
		},
		{
			name:    "success false is an error",
			do:      func(c *Client) error { return c.Set(1, rgb.Colour{}) },
			replies: map[string][]byte{"SetRGBLed": nil},
			want:    call{"SetRGBLed", fields(1, 0, 0, 0, 0)},
			wantErr: "SetRGBLed: keymapp reported failure",
		},
		{
			name:    "a gRPC error comes back",
			do:      func(c *Client) error { return c.RestoreAll() },
			want:    call{"SetRGBAll", fields(0, 0, 0, 1)},
			wantErr: "Unimplemented",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, f := serve(t, tt.replies)
			c, err := Dial(path)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			err = tt.do(c)
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
			got := f.got()
			if len(got) != 1 || got[0].method != tt.want.method || !bytes.Equal(got[0].req, tt.want.req) {
				t.Errorf("calls = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestKeyboard(t *testing.T) {
	kb := append([]byte{0x0a, 14}, "Moonlander MK1"...)
	connected := append([]byte{0x0a, 3}, "1.3"...)
	connected = append(append(connected, 0x12, byte(len(kb))), kb...)
	tests := []struct {
		name    string
		reply   []byte
		want    string
		wantErr string
	}{
		{"connected", connected, "Moonlander MK1", ""},
		{"none connected", append([]byte{0x0a, 3}, "1.3"...), "", "no keyboard connected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, _ := serve(t, map[string][]byte{"GetStatus": tt.reply})
			c, err := Dial(path)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			got, err := c.Keyboard()
			if got != tt.want || (tt.wantErr == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Keyboard() = %q, %v; want %q, %q", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestDialMissingSocket(t *testing.T) {
	_, err := Dial(filepath.Join(t.TempDir(), "none.sock"))
	if err == nil || !strings.Contains(err.Error(), "is Keymapp running") {
		t.Errorf("err = %v", err)
	}
}

func TestSocketPath(t *testing.T) {
	t.Setenv("KEYMAPP_SOCKET", "/run/k.sock")
	if p, err := SocketPath(); err != nil || p != "/run/k.sock" {
		t.Errorf("$KEYMAPP_SOCKET: %q, %v", p, err)
	}
	t.Setenv("KEYMAPP_SOCKET", "")
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	t.Setenv("HOME", "/home/u")
	p, err := SocketPath()
	if err != nil || !strings.HasSuffix(p, filepath.Join(".keymapp", "keymapp.sock")) {
		t.Errorf("default: %q, %v", p, err)
	}
}
