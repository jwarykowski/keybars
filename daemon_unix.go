//go:build unix

package main

import (
	"errors"
	"syscall"
)

// detached starts the background keybars in its own session, so closing the
// terminal doesn't stop it.
func detached() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func terminate(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }
