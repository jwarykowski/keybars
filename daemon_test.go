//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strconv"
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
