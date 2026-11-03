//go:build unix

package main

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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

func TestStartOnce(t *testing.T) {
	sock, _ := fakeKeymapp(t, "Planck EZ", 100)
	background(t)
	t.Cleanup(func() { _, _ = stdout(t, stop) })
	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- start([]string{"-socket", sock}) }()
	}
	var started int
	for range 2 {
		if err := <-errs; err == nil {
			started++
		} else if !strings.Contains(err.Error(), "already running") {
			t.Errorf("err = %v", err)
		}
	}
	if started != 1 {
		t.Errorf("%d starts launched keybars, want 1", started)
	}
}

func TestStartRefusesStdin(t *testing.T) {
	sock, _ := fakeKeymapp(t, "Planck EZ", 100)
	background(t)
	cfg := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(cfg, []byte("stdin = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := stdout(t, func() error { return start([]string{"-socket", sock, "-config", cfg}) })
	if err == nil || !strings.Contains(err.Error(), "-stdin can't run in the background") {
		t.Errorf("err = %v", err)
	}
}

func TestStartQuietExit(t *testing.T) {
	sock, _ := fakeKeymapp(t, "Planck EZ", 100)
	background(t)
	// a cava that ends at once, so keybars exits without an error
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "cava"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// a race build can take over a second to get going
	old := startWait
	startWait = 10 * time.Second
	t.Cleanup(func() { startWait = old })
	_, err := stdout(t, func() error { return start([]string{"-socket", sock}) })
	if err == nil || !strings.Contains(err.Error(), "without an error") {
		t.Errorf("err = %v", err)
	}
}

func TestStartLockFails(t *testing.T) {
	background(t)
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "keybars")
	if err := os.MkdirAll(filepath.Join(dir, "keybars.lock"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := start(nil); err == nil || !strings.Contains(err.Error(), "keybars.lock") {
		t.Errorf("err = %v", err)
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

func TestHomeFallbacks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if dir, err := stateDir(); err != nil || dir != filepath.Join(home, ".local", "state", "keybars") {
		t.Errorf("stateDir = %q, %v", dir, err)
	}
	if p, err := configPath(); err != nil || p != filepath.Join(home, ".config", "keybars", "config") {
		t.Errorf("configPath = %q, %v", p, err)
	}
}

func TestNoHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	for name, fn := range map[string]func() error{
		"start":  func() error { return start(nil) },
		"stop":   stop,
		"status": status,
	} {
		if err := fn(); err == nil {
			t.Errorf("%s without a home: no error", name)
		}
	}
	// without a home there's no default settings file, which is fine
	if err := loadConfig(flag.NewFlagSet("t", flag.ContinueOnError), nil); err != nil {
		t.Errorf("loadConfig: %v", err)
	}
}

func TestOurs(t *testing.T) {
	if ours(999999999) {
		t.Error("a pid that's gone is ours")
	}
	if !ours(os.Getpid()) {
		t.Error("this program isn't ours")
	}
	t.Setenv("PATH", "")
	if !ours(999999999) {
		t.Error("without ps, the pid file isn't trusted")
	}
}

func TestStartStateDirUnwritable(t *testing.T) {
	background(t)
	// a file where the state directory should be
	file := filepath.Join(os.Getenv("XDG_STATE_HOME"), "keybars")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := start(nil); err == nil {
		t.Error("no error")
	}
}
