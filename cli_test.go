package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captured runs cli with args and returns its exit status and what it
// printed to stdout and stderr.
func captured(t *testing.T, args ...string) (code int, out, errOut string) {
	t.Helper()
	pipe := func(f **os.File) (restore func() string) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		old := *f
		*f = w
		done := make(chan string)
		go func() {
			b, _ := io.ReadAll(r)
			done <- string(b)
		}()
		return func() string {
			*f = old
			w.Close()
			return <-done
		}
	}
	stdout, stderr := pipe(&os.Stdout), pipe(&os.Stderr)
	code = cli(args)
	return code, stdout(), stderr()
}

func TestCLI(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("KEYMAPP_SOCKET", filepath.Join(t.TempDir(), "none.sock"))
	tests := []struct {
		args     []string
		code     int
		out, err string
	}{
		{[]string{"version"}, 0, "keybars ", ""},
		{[]string{"--version"}, 0, "keybars ", ""},
		{[]string{"licences"}, 0, "Third-party notices", ""},
		{[]string{"licenses"}, 0, "Third-party notices", ""},
		{[]string{"help"}, 0, "usage:", ""},
		{[]string{"frobnicate"}, 2, "", "usage:"},
		{[]string{"status"}, 0, "isn't running", ""},
		{[]string{"stop"}, 0, "isn't running", ""},
		{[]string{"maps"}, 0, "moonlander", ""},
		{[]string{"test", "-h"}, 0, "", "-step"},
		{[]string{"start", "-h"}, 0, "", "-gain"},
		{[]string{"find", "-backend", "razer"}, 1, "", "keybars: -backend"},
		{[]string{"gen", "-order", "sideways", "x"}, 1, "", "keybars: "},
		{[]string{"-gain", "0"}, 1, "", "keybars: -gain 0"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			code, out, errOut := captured(t, tt.args...)
			if code != tt.code || !strings.Contains(out, tt.out) || !strings.Contains(errOut, tt.err) {
				t.Errorf("exit %d, stdout %.80q, stderr %.80q; want %d, %q, %q", code, out, errOut, tt.code, tt.out, tt.err)
			}
		})
	}
}

func TestCommandErrors(t *testing.T) {
	sock, _ := fakeKeymapp(t, "Planck EZ", 100)
	none := filepath.Join(t.TempDir(), "none.sock")
	planck := filepath.Join("pkg", "qmk", "testdata", "planck.json")
	tests := []struct {
		name    string
		fn      func([]string) error
		args    []string
		wantErr string
	}{
		{"run: unknown flag", run, []string{"-bogus"}, "-bogus"},
		{"run: bad config", run, []string{"-config", filepath.Join(t.TempDir(), "none")}, "no such file"},
		{"test: unknown flag", test, []string{"-bogus"}, "-bogus"},
		{"test: no Keymapp", test, []string{"-socket", none}, "is Keymapp running"},
		{"test: a map that isn't one", test, []string{"-socket", sock, "-map", planck}, "not an LED index"},
		{"find: unknown flag", find, []string{"-bogus"}, "-bogus"},
		{"find: no Keymapp", find, []string{"-socket", none}, "is Keymapp running"},
		{"maps: unknown flag", listMaps, []string{"-bogus"}, "-bogus"},
		{"maps: -save without OpenRGB", listMaps, []string{"-save", t.TempDir()}, "-save is for -backend openrgb"},
		{"maps: no OpenRGB", listMaps, []string{"-backend", "openrgb", "-openrgb", "127.0.0.1:1"}, "is OpenRGB running"},
		{"gen: unknown flag", gen, []string{"-bogus"}, "-bogus"},
		{"gen: unknown order", gen, []string{"-order", "sideways", planck}, "sideways"},
		{"gen: unknown layout", gen, []string{"-layout", "LAYOUT_none", planck}, "LAYOUT_none"},
		{"gen -json: unknown layout", gen, []string{"-json", "-layout", "LAYOUT_none", planck}, "LAYOUT_none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := stdout(t, func() error { return tt.fn(tt.args) })
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
