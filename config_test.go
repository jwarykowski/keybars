package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testFlags() (*flag.FlagSet, *string, *float64, *bool, *time.Duration) {
	fl := flag.NewFlagSet("test", flag.ContinueOnError)
	fl.SetOutput(new(strings.Builder))
	source := fl.String("source", "", "")
	gain := fl.Float64("gain", 1.5, "")
	peaks := fl.Bool("peaks", true, "")
	idle := fl.Duration("idle", time.Second, "")
	fl.String("config", "", "")
	return fl, source, gain, peaks, idle
}

func TestApplyConfig(t *testing.T) {
	fl, source, gain, peaks, idle := testFlags()
	cfg := `# keybars
source = BlackHole 2ch

gain=2
peaks = false
idle = "3s"
`
	if err := applyConfig(fl, "config", strings.NewReader(cfg)); err != nil {
		t.Fatal(err)
	}
	if *source != "BlackHole 2ch" || *gain != 2 || *peaks || *idle != 3*time.Second {
		t.Errorf("got source %q, gain %v, peaks %v, idle %v", *source, *gain, *peaks, *idle)
	}
}

func TestApplyConfigErrors(t *testing.T) {
	for cfg, want := range map[string]string{
		"gain = 2\nvolume = 11\n": `config:2: unknown setting "volume"`,
		"gain = loud\n":           "config:1: gain:",
		"just words\n":            "config:1: want name = value",
		"config = other\n":        `unknown setting "config"`,
	} {
		fl, _, _, _, _ := testFlags()
		err := applyConfig(fl, "config", strings.NewReader(cfg))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", cfg, err, want)
		}
	}
}

func TestFlagsOverrideConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("gain = 2\nsource = BlackHole 2ch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fl, source, gain, _, _ := testFlags()
	args := []string{"-config", path, "-gain", "3"}
	if err := loadConfig(fl, args); err != nil {
		t.Fatal(err)
	}
	if err := fl.Parse(args); err != nil {
		t.Fatal(err)
	}
	if *gain != 3 || *source != "BlackHole 2ch" {
		t.Errorf("got gain %v, source %q", *gain, *source)
	}
}

func TestLoadConfigMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fl, _, _, _, _ := testFlags()
	if err := loadConfig(fl, nil); err != nil {
		t.Errorf("missing default config: %v", err)
	}
	if err := loadConfig(fl, []string{"-config=/no/such/file"}); err == nil {
		t.Error("missing -config file: no error")
	}
}

func TestConfigArg(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-gain", "2"}, ""},
		{[]string{"-config", "a.conf"}, "a.conf"},
		{[]string{"--config=b.conf", "-v"}, "b.conf"},
		{[]string{"--", "-config", "c.conf"}, ""},
	} {
		if got := configArg(tc.args); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.args, got, tc.want)
		}
	}
}
