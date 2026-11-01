//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRunning(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "keybars.pid")
	if _, ok := running(dir); ok {
		t.Error("no pid file: running")
	}
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if pid, ok := running(dir); !ok || pid != os.Getpid() {
		t.Errorf("live pid: got %d, %v", pid, ok)
	}
	if err := os.WriteFile(pidFile, []byte("999999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := running(dir); ok {
		t.Error("dead pid: running")
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Error("dead pid: pid file left behind")
	}
}

// background sets up start to run this test binary as keybars, with a cava
// that plays the same frame over and over.
func background(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	cava := "#!/bin/sh\nwhile :; do echo '8;16;24;0;8;16;24;0;8;16;24;0;'; sleep 0.05; done\n"
	if err := os.WriteFile(filepath.Join(bin, "cava"), []byte(cava), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KEYBARS_TEST_MAIN", "1")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

func TestStartStop(t *testing.T) {
	sock, k := fakeKeymapp(t, "Planck EZ", 100)
	background(t)
	t.Cleanup(func() { _, _ = stdout(t, stop) })
	steps := []struct {
		name    string
		cmd     func() error
		want    string
		wantErr string
	}{
		{"start", func() error { return start([]string{"-socket", sock}) }, "keybars started", ""},
		{"start again", func() error { return start([]string{"-socket", sock}) }, "", "already running"},
		{"status", status, "keybars is running", ""},
		{"stop", stop, "keybars stopped", ""},
		{"status after", status, "keybars isn't running", ""},
	}
	for _, s := range steps {
		out, err := stdout(t, s.cmd)
		if s.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), s.wantErr) {
				t.Fatalf("%s: err = %v, want %q", s.name, err, s.wantErr)
			}
			continue
		}
		if err != nil || !strings.Contains(out, s.want) {
			t.Fatalf("%s: %q, %v; want %q", s.name, out, err, s.want)
		}
	}
	if k.count("SetRGBLed") == 0 {
		t.Error("the background keybars lit nothing")
	}
	if k.count("SetRGBAll") == 0 {
		t.Error("stop didn't hand the keys back to the layout")
	}
}

func TestStartReportsEarlyExit(t *testing.T) {
	background(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, err := stdout(t, func() error { return start([]string{"-gain", "0"}) })
	if err == nil || !strings.Contains(err.Error(), "exited at start") || !strings.Contains(err.Error(), "-gain 0") {
		t.Errorf("err = %v", err)
	}
	if _, ok := running(os.Getenv("XDG_STATE_HOME") + "/keybars"); ok {
		t.Error("left a pid file")
	}
}
