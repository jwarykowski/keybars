package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestParseFind(t *testing.T) {
	tests := []struct {
		line string
		want findCmd
	}{
		{"", findCmd{op: 'n'}},
		{"  ", findCmd{op: 'n'}},
		{"q", findCmd{op: 'q'}},
		{"B", findCmd{op: 'b'}},
		{"help", findCmd{op: '?'}},
		{"g 12", findCmd{op: 'g', led: 12}},
		{"2 5", findCmd{op: 'p', row: 1, cols: []int{4}}},
		{"5 4-6", findCmd{op: 'p', row: 4, cols: []int{3, 4, 5}}},
	}
	for _, tt := range tests {
		got, err := parseFind(tt.line)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parseFind(%q) = %+v, %v; want %+v", tt.line, got, err, tt.want)
		}
	}
	for _, bad := range []string{"x", "0 1", "1 0", "2 5-3", "g", "g -1", "1 2 3", "2 a"} {
		if _, err := parseFind(bad); err == nil {
			t.Errorf("parseFind(%q) took it", bad)
		}
	}
}

func TestFindWritesTheMap(t *testing.T) {
	sock, _ := fakeKeymapp(t, "Mystery 9000", 10)
	path := filepath.Join(t.TempDir(), "my-board.txt")
	// LED 1 top left, 2 placed then moved after going back, 3 a wide key
	in := "1 1\n1 2\nb\n1 3\n2 1-2\nq\n"
	var out string
	err := withStdin(t, in, func() error {
		var err error
		out, err = stdout(t, func() error { return find([]string{"-socket", sock, "-o", path}) })
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Mystery 9000: written by keybars find\n 1  -  2\n 3  3  -\n"
	if string(b) != want {
		t.Errorf("map:\n%s\nwant:\n%s", b, want)
	}
	if !strings.Contains(out, "Wrote "+path+" (4 keys). Check it: keybars test -map "+path) {
		t.Errorf("output:\n%s", out)
	}
}

func TestFindPrintsTheMap(t *testing.T) {
	sock, _ := fakeKeymapp(t, "Mystery 9000", 10)
	var out string
	err := withStdin(t, "1 1\n", func() error {
		var err error
		out, err = stdout(t, func() error { return find([]string{"-socket", sock}) })
		return err
	})
	if err != nil || !strings.Contains(out, "pass as -map FILE") || !strings.HasSuffix(out, " 1\n") {
		t.Errorf("err = %v, output:\n%s", err, out)
	}
}

func TestFindTriesAgain(t *testing.T) {
	sock, k := fakeKeymapp(t, "Planck EZ", 5)
	k.flaky = 3
	out, err := stdout(t, func() error { return find([]string{"-socket", sock, "-auto", "1ms"}) })
	if err != nil || !strings.Contains(out, "LED 3") || !strings.Contains(out, "no LED 6: done") {
		t.Errorf("err = %v, output:\n%s", err, out)
	}
}

func TestFindRefuses(t *testing.T) {
	sock, _ := fakeKeymapp(t, "Planck EZ", 5)
	for _, tt := range []struct {
		args    []string
		wantErr string
	}{
		{[]string{"-from", "5", "-to", "2"}, "-to 2 is before -from 5"},
		{[]string{"-from", "-5"}, "want LED indices"},
		{[]string{"-auto", "-1s"}, "-auto"},
	} {
		if err := find(append([]string{"-socket", sock}, tt.args...)); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("find %v: err = %v, want %q", tt.args, err, tt.wantErr)
		}
	}
}

// TestRefusesWhileInBackground: find and test light keys a background
// keybars would paint over.
func TestRefusesWhileInBackground(t *testing.T) {
	sock, _ := fakeKeymapp(t, "Planck EZ", 100)
	background(t)
	t.Cleanup(func() { _, _ = stdout(t, stop) })
	if _, err := stdout(t, func() error { return start([]string{"-socket", sock}) }); err != nil {
		t.Fatal(err)
	}
	for name, fn := range map[string]func([]string) error{"find": find, "test": test} {
		if err := fn([]string{"-socket", sock}); err == nil || !strings.Contains(err.Error(), "keybars stop first") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// TestFindCtrlC: Ctrl-C stops find while it waits for Enter, and gives the
// keys back.
func TestFindCtrlC(t *testing.T) {
	sock, k := fakeKeymapp(t, "Planck EZ", 100)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cmd := exec.Command(os.Args[0], "find", "-socket", sock)
	cmd.Env = append(os.Environ(), "KEYBARS_TEST_MAIN=1")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); k.count("SetRGBLed") == 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("find never lit an LED")
		}
	}
	_ = cmd.Process.Signal(syscall.SIGINT)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("still running 5 s after Ctrl-C")
	}
	if k.count("SetRGBAll") != 2 {
		t.Errorf("%d SetRGBAll calls, want 2: darken all, then restore", k.count("SetRGBAll"))
	}
}
