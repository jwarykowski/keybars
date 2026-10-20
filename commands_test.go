package main

import (
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// stdout captures what fn prints.
func stdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	out := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		out <- string(b)
	}()
	ferr := fn()
	os.Stdout = old
	w.Close()
	return <-out, ferr
}

func TestGen(t *testing.T) {
	planck := filepath.Join("pkg", "qmk", "testdata", "planck.json")
	tests := []struct {
		name    string
		args    []string
		want    []string
		wantErr string
	}{
		{"keymapp order", []string{planck}, []string{"keymapp index for each key", "Keymapp never lights index 0"}, ""},
		{"firmware order", []string{"-order", "firmware", planck}, []string{"firmware index for each key"}, ""},
		{"shape as JSON", []string{"-json", planck}, []string{`"keys"`}, ""},
		{"bad order", []string{"-order", "zigzag", planck}, nil, "zigzag"},
		{"no keyboard", nil, nil, flag.ErrHelp.Error()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stdout(t, func() error { return gen(tt.args) })
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("output lacks %q:\n%s", w, got)
				}
			}
		})
	}
}

func TestStopAndStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("background keybars is unix only")
	}
	tests := []struct {
		name string
		pid  func(t *testing.T) string // the pid file's contents, "" for none
		want string
	}{
		{"nothing running", func(*testing.T) string { return "" }, "keybars isn't running"},
		{"pid file of a dead process", func(*testing.T) string { return "999999999" }, "keybars isn't running"},
		{"garbage pid file", func(*testing.T) string { return "not a pid" }, "keybars isn't running"},
		{"pid reused by another program", func(t *testing.T) string {
			// a stale pid file must never stop someone else's process
			other := exec.Command("sleep", "30")
			if err := other.Start(); err != nil {
				t.Skip(err)
			}
			t.Cleanup(func() { _ = other.Process.Kill(); _ = other.Wait() })
			return strconv.Itoa(other.Process.Pid)
		}, "keybars isn't running"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := t.TempDir()
			t.Setenv("XDG_STATE_HOME", state)
			pidFile := filepath.Join(state, "keybars", "keybars.pid")
			if pid := tt.pid(t); pid != "" {
				if err := os.MkdirAll(filepath.Dir(pidFile), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(pidFile, []byte(pid+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for name, cmd := range map[string]func() error{"status": status, "stop": stop} {
				got, err := stdout(t, cmd)
				if err != nil || strings.TrimSpace(got) != tt.want {
					t.Errorf("%s: %q, %v; want %q", name, got, err, tt.want)
				}
			}
			if _, err := os.Stat(pidFile); !errors.Is(err, os.ErrNotExist) {
				t.Error("stale pid file left behind")
			}
		})
	}
}
