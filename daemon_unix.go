//go:build unix

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// detached starts the background keybars in its own session, so closing the
// terminal doesn't stop it.
func detached() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// ours reports whether pid runs this program, so a pid file left from before
// a reboot never names another process to stop. Without ps it trusts the file.
func ours(pid int) bool {
	exe, err := os.Executable()
	if err != nil {
		return true
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	if err != nil {
		return false // ps exits 1 for a pid that's gone
	}
	return sameProgram(strings.TrimSpace(string(out)), filepath.Base(exe))
}

// sameProgram compares ps's command name with an executable's: Linux cuts
// the name to 15 bytes, macOS gives the whole path.
func sameProgram(comm, exe string) bool {
	comm = filepath.Base(comm)
	if len(comm) == 15 && len(exe) > 15 {
		return strings.HasPrefix(exe, comm)
	}
	return comm == exe
}

func terminate(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }
