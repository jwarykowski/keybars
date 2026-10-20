package source

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    []int
		wantErr bool
	}{
		{"cava frame", "3;0;12;\n", []int{3, 0, 12}, false},
		{"no trailing delimiter", "1;2", []int{1, 2}, false},
		{"empty line", "", nil, false},
		{"only delimiters", ";;;", nil, false},
		{"surrounding space", "  4;5;  ", []int{4, 5}, false},
		{"negative passes through", "-1;2;", []int{-1, 2}, false},
		{"not a number", "3;x;", nil, true},
		{"inner space", "3; 4;", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.line)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parse(%q) err = %v, wantErr %v", tt.line, err, tt.wantErr)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Parse(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestRead(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    [][]int
		wantErr bool
	}{
		{"frames", "1;2;\n3;4;\n", [][]int{{1, 2}, {3, 4}}, false},
		{"nothing", "", nil, false},
		{"stops at a bad frame", "1;2;\nx;\n3;4;\n", [][]int{{1, 2}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frames := make(chan []int, 4)
			err := Read(strings.NewReader(tt.in), frames)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			var got [][]int
			for f := range frames {
				got = append(got, f)
			}
			if !slices.EqualFunc(got, tt.want, slices.Equal) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// fakeCava puts a cava on PATH that runs script, with $1 and $2 being cava's
// "-p FILE".
func fakeCava(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake cava is a shell script")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cava"), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// collect runs Cava to the end and returns its frames.
func collect(t *testing.T, ctx context.Context, source string) ([][]int, error) {
	t.Helper()
	frames := make(chan []int)
	errc := make(chan error, 1)
	go func() { errc <- Cava(ctx, 4, 8, 30, 35, source, frames) }()
	var got [][]int
	timeout := time.After(5 * time.Second)
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				return got, <-errc
			}
			got = append(got, f)
		case <-timeout:
			t.Fatal("frames left open")
		}
	}
}

func TestCava(t *testing.T) {
	tests := []struct {
		name    string
		script  string
		source  string
		want    [][]int
		wantErr string
	}{
		{"streams frames", `printf '1;2;3;4;\n5;6;7;8;\n'`, "", [][]int{{1, 2, 3, 4}, {5, 6, 7, 8}}, ""},
		{"crash is reported", `printf '1;\n'; exit 3`, "", [][]int{{1}}, "cava: exit status 3"},
		{"bad output is reported", `printf 'oops\n'`, "", nil, `bad frame "oops"`},
		{"source in the config", `grep -q '^source = BlackHole 2ch$' "$2" && printf '9;\n'`, "BlackHole 2ch", [][]int{{9}}, ""},
		{"config asks for the grid", `grep -q '^bars = 4$' "$2" && grep -q '^ascii_max_range = 8$' "$2" && printf '1;\n'`, "", [][]int{{1}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeCava(t, tt.script)
			got, err := collect(t, context.Background(), tt.source)
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
			if !slices.EqualFunc(got, tt.want, slices.Equal) {
				t.Errorf("frames = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCavaStopsWithContext(t *testing.T) {
	fakeCava(t, `printf '1;\n'; exec sleep 30`)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	if _, err := collect(t, ctx, ""); err != nil {
		t.Errorf("a cancelled cava is no error: %v", err)
	}
}

// Cava must close frames on every return, or the CLI's scaling goroutine
// ranges over it forever and keybars hangs with the error unread.
func TestCavaClosesFramesOnFailure(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T)
		source string
	}{
		{"cava not installed", func(t *testing.T) { t.Setenv("PATH", "") }, ""},
		{"temp dir unusable", func(t *testing.T) { t.Setenv("TMPDIR", "/nonexistent/keybars") }, ""},
		{"line break in the source", func(*testing.T) {}, "x\n[output]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			got, err := collect(t, context.Background(), tt.source)
			if err == nil || got != nil {
				t.Errorf("got %v, %v; want no frames and an error", got, err)
			}
		})
	}
}
