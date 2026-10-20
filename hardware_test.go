package main

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

// rawCodec passes gRPC messages through as their encoded bytes.
type rawCodec struct{}

func (rawCodec) Marshal(v any) ([]byte, error) { return *v.(*[]byte), nil }
func (rawCodec) Unmarshal(b []byte, v any) error {
	*v.(*[]byte) = append([]byte(nil), b...)
	return nil
}
func (rawCodec) Name() string { return "proto" }

// keymappd is a fake Keymapp on a unix socket with a keyboard called name
// connected ("" for none). LEDs from last up don't exist.
type keymappd struct {
	mu    sync.Mutex
	calls map[string]int
	lit   []int // each LED SetRGBLed was asked to light, in order
}

func fakeKeymapp(t *testing.T, name string, last int) (string, *keymappd) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Keymapp's socket is a unix socket")
	}
	k := &keymappd{calls: map[string]int{}}
	path := filepath.Join(t.TempDir(), "k.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ok := []byte{0x08, 1}
	s := grpc.NewServer(grpc.ForceServerCodec(rawCodec{}), grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		full, _ := grpc.MethodFromServerStream(stream)
		method := strings.TrimPrefix(full, "/api.KeyboardService/")
		var req []byte
		if err := stream.RecvMsg(&req); err != nil {
			return err
		}
		k.mu.Lock()
		k.calls[method]++
		k.mu.Unlock()
		var reply []byte
		switch method {
		case "GetStatus":
			if name != "" {
				inner := append([]byte{0x0a, byte(len(name))}, name...)
				reply = append([]byte{0x12, byte(len(inner))}, inner...)
			}
		case "SetRGBAll":
			reply = ok
		case "SetRGBLed":
			led := 0
			if len(req) >= 2 && req[0] == 0x08 {
				led = int(req[1])
			}
			if led > last {
				return stream.SendMsg(new([]byte)) // success false
			}
			k.mu.Lock()
			k.lit = append(k.lit, led)
			k.mu.Unlock()
			reply = ok
		default:
			return grpcstatus.Error(codes.Unimplemented, method)
		}
		return stream.SendMsg(&reply)
	}))
	go func() { _ = s.Serve(l) }()
	t.Cleanup(s.Stop)
	// keep the user's settings file out of it
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	return path, k
}

func (k *keymappd) count(method string) int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.calls[method]
}

// withStdin runs fn with stdin reading input.
func withStdin(t *testing.T, input string, fn func() error) error {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = w.WriteString(input)
		w.Close()
	}()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old; r.Close() }()
	return fn()
}

func TestRunFromStdin(t *testing.T) {
	sock, k := fakeKeymapp(t, "Planck EZ", 100)
	// a slow keyboard skips to the newest frame, so only the last is sure to
	// be drawn
	frames := strings.Repeat("3;4;2;1;0;4;3;2;1;0;1;2\n", 5)
	err := withStdin(t, frames, func() error {
		return run([]string{"-socket", sock, "-stdin", "-peaks=false", "-idle", "1h"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if k.count("SetRGBLed") == 0 {
		t.Error("no LEDs were lit")
	}
	if k.count("SetRGBAll") == 0 {
		t.Error("the keys weren't handed back to the layout at the end")
	}
}

func TestRunRefuses(t *testing.T) {
	sock, _ := fakeKeymapp(t, "", 100)
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"bad reserve", []string{"-reserve", "1,x"}, "-reserve"},
		{"bad colours", []string{"-colours", "green"}, "green"},
		{"bad peak colour", []string{"-model", "planck", "-peak-colour", "pink"}, "-peak-colour"},
		{"no keyboard to detect a map for", nil, "no keyboard connected to Keymapp"},
		{"too tall", []string{"-model", "planck", "-height", "40"}, "height"},
		{"bad backend", []string{"-backend", "razer"}, "want keymapp or openrgb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(append([]string{"-socket", sock, "-stdin"}, tt.args...))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestTestSweeps(t *testing.T) {
	sock, k := fakeKeymapp(t, "Planck EZ", 100)
	out, err := stdout(t, func() error { return test([]string{"-socket", sock, "-step", "1ms"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "planck: ") || !strings.Contains(out, "columns left to right") {
		t.Errorf("output: %q", out)
	}
	if k.count("SetRGBLed") == 0 || k.count("SetRGBAll") == 0 {
		t.Errorf("%d LEDs set, %d clears", k.count("SetRGBLed"), k.count("SetRGBAll"))
	}
}

func TestFind(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		stdin   string
		want    []string
		wantLit []int
	}{
		{"steps on a timer until told to stop", []string{"-auto", "1ms", "-to", "3"}, "", []string{"Planck EZ: every key goes dark", "LED 1", "LED 3"}, []int{1, 1, 2, 2, 3, 3}},
		{"stops where the keyboard ends", []string{"-auto", "1ms", "-from", "4"}, "", []string{"LED 5", "no LED 6: done"}, []int{4, 4, 5, 5}},
		{"Enter steps, q stops", nil, "\nq\n", []string{"Enter for the next", "LED 2"}, []int{1, 1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sock, k := fakeKeymapp(t, "Planck EZ", 5)
			var out string
			err := withStdin(t, tt.stdin, func() error {
				var err error
				out, err = stdout(t, func() error { return find(append([]string{"-socket", sock}, tt.args...)) })
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("output lacks %q:\n%s", w, out)
				}
			}
			// each LED is sent twice: lit, then dark again
			k.mu.Lock()
			lit := slices.Clone(k.lit)
			k.mu.Unlock()
			if !slices.Equal(lit, tt.wantLit) {
				t.Errorf("LEDs set %v, want %v", lit, tt.wantLit)
			}
			if k.count("SetRGBAll") != 2 {
				t.Errorf("%d SetRGBAll calls, want 2: darken all, then restore", k.count("SetRGBAll"))
			}
		})
	}
}

func TestListMaps(t *testing.T) {
	tests := []struct {
		name, keyboard string
		socket         bool
		want           string
	}{
		{"a known keyboard", "Voyager", true, "connected: Voyager (uses voyager)"},
		{"an unknown one", "Mystery 9000", true, "connected: Mystery 9000 (no built-in map"},
		{"none connected", "", true, "planck"},
		{"no Keymapp", "", false, "planck"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sock, _ := fakeKeymapp(t, tt.keyboard, 100)
			if !tt.socket {
				sock = filepath.Join(t.TempDir(), "none.sock")
			}
			out, err := stdout(t, func() error { return listMaps([]string{"-socket", sock}) })
			if err != nil || !strings.Contains(out, tt.want) {
				t.Errorf("maps: %v\n%s\nwant %q", err, out, tt.want)
			}
			if tt.keyboard == "" && strings.Contains(out, "connected:") {
				t.Errorf("claims a keyboard: %s", out)
			}
		})
	}
}
