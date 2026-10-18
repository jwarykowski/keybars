package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// stateDir holds the background keybars' pid file and log:
// $XDG_STATE_HOME/keybars, else ~/.local/state/keybars.
func stateDir() (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "keybars"), nil
}

// running is the pid of the background keybars, removing a pid file left by
// one that has exited.
func running(dir string) (int, bool) {
	pidFile := filepath.Join(dir, "keybars.pid")
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || !alive(pid) {
		os.Remove(pidFile)
		return 0, false
	}
	return pid, true
}

// start runs keybars with args in the background, logging to the state dir.
// It waits a moment so a keybars that fails at once (no Keymapp, a bad
// setting) reports why here.
func start(args []string) error {
	if slices.ContainsFunc(args, func(a string) bool { return a == "-h" || a == "-help" || a == "--help" }) {
		return run(args)
	}
	dir, err := stateDir()
	if err != nil {
		return err
	}
	if pid, ok := running(dir); ok {
		return fmt.Errorf("already running (pid %d)", pid)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := filepath.Join(dir, "keybars.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer logFile.Close()

	cmd := exec.Command(exe, append([]string{"run"}, args...)...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = detached()
	if err := cmd.Start(); err != nil {
		return err
	}
	pidFile := filepath.Join(dir, "keybars.pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		_ = cmd.Process.Kill() // the pid file error is the one to report
		return err
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }() // only whether it exits matters here
	select {
	case <-exited:
		os.Remove(pidFile)
		out, _ := os.ReadFile(logPath)
		return fmt.Errorf("exited at start:\n%s", strings.TrimSpace(string(out)))
	case <-time.After(time.Second):
	}
	fmt.Printf("keybars started (pid %d), log in %s\n", cmd.Process.Pid, logPath)
	return nil
}

// stop asks the background keybars to exit as Ctrl-C does, so it hands the
// keys back to the layout, and waits for it.
func stop() error {
	dir, err := stateDir()
	if err != nil {
		return err
	}
	pid, ok := running(dir)
	if !ok {
		fmt.Println("keybars isn't running")
		return nil
	}
	if err := terminate(pid); err != nil {
		return err
	}
	for deadline := time.Now().Add(5 * time.Second); alive(pid); {
		if time.Now().After(deadline) {
			return fmt.Errorf("pid %d didn't stop", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := os.Remove(filepath.Join(dir, "keybars.pid")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	fmt.Println("keybars stopped")
	return nil
}

func status() error {
	dir, err := stateDir()
	if err != nil {
		return err
	}
	if pid, ok := running(dir); ok {
		fmt.Printf("keybars is running (pid %d), log in %s\n", pid, filepath.Join(dir, "keybars.log"))
	} else {
		fmt.Println("keybars isn't running")
	}
	return nil
}
