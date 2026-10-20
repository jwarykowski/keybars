//go:build !unix

package main

import (
	"errors"
	"syscall"
)

func detached() *syscall.SysProcAttr { return nil }

func alive(int) bool { return false }

func terminate(int) error { return errors.New("stop isn't supported on this system") }

func ours(int) bool { return true }
