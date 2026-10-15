package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// configPath is $XDG_CONFIG_HOME/keybars/config, else ~/.config/keybars/config.
func configPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "keybars", "config"), nil
}

// configArg finds -config FILE or -config=FILE in args before fl parses them,
// so the file's settings can go in first and flags override them.
func configArg(args []string) string {
	for i, a := range args {
		if a == "--" {
			break
		}
		name := strings.TrimLeft(a, "-")
		if name == a {
			continue
		}
		if v, ok := strings.CutPrefix(name, "config="); ok {
			return v
		}
		if name == "config" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// loadConfig sets fl's flags from the config file named in args, else the
// default one, which may be missing.
func loadConfig(fl *flag.FlagSet, args []string) error {
	path := configArg(args)
	explicit := path != ""
	if !explicit {
		var err error
		if path, err = configPath(); err != nil {
			return nil
		}
	}
	f, err := os.Open(path)
	if err != nil {
		if !explicit && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	defer f.Close()
	return applyConfig(fl, path, f)
}

// proOnly are keybars Pro's settings, which share this file; keybars skips
// them rather than stopping, so one file serves both.
var proOnly = map[string]bool{"low-hz": true, "high-hz": true, "brightness": true, "symmetry": true}

// applyConfig reads "name = value" lines, one flag each by its own name; #
// starts a comment line and quotes around a value are dropped.
func applyConfig(fl *flag.FlagSet, name string, r io.Reader) error {
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), unquote(strings.TrimSpace(value))
		if !ok || key == "" {
			return fmt.Errorf("%s:%d: want name = value, got %q", name, n, line)
		}
		if proOnly[key] && fl.Lookup(key) == nil {
			continue
		}
		if key == "config" || fl.Lookup(key) == nil {
			return fmt.Errorf("%s:%d: unknown setting %q", name, n, key)
		}
		if err := fl.Set(key, value); err != nil {
			return fmt.Errorf("%s:%d: %s: %w", name, n, key, err)
		}
	}
	return sc.Err()
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
