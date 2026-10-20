package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jwarykowski/keybars/pkg/bars"
	"github.com/jwarykowski/keybars/pkg/layout"
	"github.com/jwarykowski/keybars/pkg/rgb"
)

func TestCheckRun(t *testing.T) {
	tests := []struct {
		name    string
		fps     int
		smooth  int
		gain    float64
		wantErr string
	}{
		{"defaults", 30, 35, 1.5, ""},
		{"edges", 1, 0, 0.01, ""},
		{"smooth at most", 30, 100, 1, ""},
		{"no frames", 0, 35, 1, "-fps 0"},
		{"smooth below", 30, -1, 1, "-smooth -1"},
		{"smooth above", 30, 101, 1, "-smooth 101"},
		{"no gain", 30, 35, 0, "-gain 0"},
		{"negative gain", 30, 35, -2, "-gain -2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkRun(tt.fps, tt.smooth, tt.gain)
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.HasPrefix(err.Error(), tt.wantErr)) {
				t.Errorf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// recorder counts what paint sends, failing Set from the failAt'th call.
type recorder struct {
	sets, restores, failAt int
}

var errUnplugged = errors.New("unplugged")

func (r *recorder) Set(int, rgb.Colour) error {
	r.sets++
	if r.failAt > 0 && r.sets >= r.failAt {
		return errUnplugged
	}
	return nil
}

func (r *recorder) RestoreAll() error { r.restores++; return nil }

// feed returns frames one at a time, then reports the source ended.
func feed(frames ...[]int) func() ([]int, bool) {
	return func() ([]int, bool) {
		if len(frames) == 0 {
			return nil, false
		}
		f := frames[0]
		frames = frames[1:]
		return f, true
	}
}

func testGrid(t *testing.T) layout.Grid {
	t.Helper()
	m, err := layout.Parse("t", strings.NewReader("0 1\n2 3\n"))
	if err != nil {
		t.Fatal(err)
	}
	g, err := layout.New(m, 2)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestPaint(t *testing.T) {
	grid := testGrid(t)
	loud, quiet := []int{2, 1}, []int{0, 0}
	tests := []struct {
		name         string
		frames       [][]int
		idle         time.Duration
		peaks        bool
		failAt       int
		wantErr      error
		wantFrames   int
		wantRestores int
	}{
		{"silence from the start paints nothing", [][]int{quiet, quiet}, time.Hour, false, 0, nil, 0, 0},
		{"music paints each frame", [][]int{loud, loud}, time.Hour, false, 0, nil, 2, 0},
		{"short silence keeps painting", [][]int{loud, quiet}, time.Hour, false, 0, nil, 2, 0},
		{"silence past idle restores once", [][]int{loud, quiet, quiet, quiet}, 0, false, 0, nil, 1, 1},
		{"music after silence paints again", [][]int{loud, quiet, loud}, 0, false, 0, nil, 2, 1},
		{"peak dots", [][]int{loud, quiet}, time.Hour, true, 0, nil, 2, 0},
		{"a failed Set stops paint", [][]int{loud, loud}, time.Hour, false, 1, errUnplugged, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{failAt: tt.failAt}
			r := bars.New(grid, rec, []rgb.Colour{{G: 255}}, nil)
			var pk *bars.Peaks
			if tt.peaks {
				r.WithPeaks(rgb.Colour{R: 255, G: 255, B: 255})
				pk = bars.NewPeaks(time.Hour, 1)
			}
			var st stats
			err := paint(context.Background(), r, pk, feed(tt.frames...), tt.idle, 0, &st)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if st.frames != tt.wantFrames || rec.restores != tt.wantRestores {
				t.Errorf("frames %d, restores %d; want %d, %d", st.frames, rec.restores, tt.wantFrames, tt.wantRestores)
			}
		})
	}
}

func TestPaintRefreshResends(t *testing.T) {
	rec := &recorder{}
	r := bars.New(testGrid(t), rec, []rgb.Colour{{G: 255}}, nil)
	loud := []int{2, 2}
	var st stats
	// a refresh of a nanosecond is due every frame, so the same frame is sent whole each time
	if err := paint(context.Background(), r, nil, feed(loud, loud, loud), time.Hour, time.Nanosecond, &st); err != nil {
		t.Fatal(err)
	}
	if st.leds != 3*4 {
		t.Errorf("sent %d LEDs, want 12", st.leds)
	}
}

func TestPaintStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := bars.New(testGrid(t), &recorder{}, []rgb.Colour{{G: 255}}, nil)
	var st stats
	if err := paint(ctx, r, nil, feed([]int{1, 1}), time.Hour, 0, &st); err != nil || st.frames != 0 {
		t.Errorf("err %v, frames %d", err, st.frames)
	}
}

func TestLatest(t *testing.T) {
	t.Run("skips to the newest frame", func(t *testing.T) {
		frames := make(chan []int)
		next := latest(context.Background(), frames)
		frames <- []int{1}
		frames <- []int{2}
		frames <- []int{3}
		close(frames)
		var got [][]int
		for {
			f, ok := next()
			if !ok {
				break
			}
			got = append(got, f)
		}
		if len(got) == 0 || !slices.Equal(got[len(got)-1], []int{3}) {
			t.Errorf("got %v, want to end with [3]", got)
		}
	})
	t.Run("ends with the context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		next := latest(ctx, make(chan []int))
		time.AfterFunc(50*time.Millisecond, cancel)
		if _, ok := next(); ok {
			t.Error("want false once ctx is done")
		}
	})
}

func TestSameProgram(t *testing.T) {
	tests := []struct {
		comm, exe string
		want      bool
	}{
		{"keybars", "keybars", true},
		{"/opt/homebrew/bin/keybars", "keybars", true},
		{"keybars.test", "keybars.test", true},
		{"vim", "keybars", false},
		{"keybars-pro-cli", "keybars", false},
		{"a-very-long-nam", "a-very-long-name-indeed", true}, // Linux cuts comm to 15
		{"a-very-long-nam", "a-very-long-nax", false},
	}
	for _, tt := range tests {
		if got := sameProgram(tt.comm, tt.exe); got != tt.want {
			t.Errorf("sameProgram(%q, %q) = %v, want %v", tt.comm, tt.exe, got, tt.want)
		}
	}
}

// qmkFake answers QMK's URLs from a map of path suffix to body; others 404.
type qmkFake map[string]string

func (f qmkFake) RoundTrip(r *http.Request) (*http.Response, error) {
	for suffix, body := range f {
		if strings.HasSuffix(r.URL.String(), suffix) {
			if body == "" {
				return nil, errors.New("connection refused")
			}
			return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}
	}
	return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
}

func TestReadQMK(t *testing.T) {
	const pad = `{"keyboard_name":"pad","layouts":{"LAYOUT":{"layout":[{"matrix":[0,0],"x":0,"y":0}]}}}`
	file := filepath.Join(t.TempDir(), "pad.json")
	if err := os.WriteFile(file, []byte(pad), 0o644); err != nil {
		t.Fatal(err)
	}
	unreadable := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		src     string
		served  qmkFake
		want    string
		wantErr string
	}{
		{"a file", file, nil, "pad", ""},
		{"QMK's API", "acme/pad", qmkFake{"v1/keyboards/acme/pad/info.json": pad}, "pad", ""},
		{"raw keyboard.json when the API lacks it", "acme/pad/", qmkFake{"keyboards/acme/pad/keyboard.json": pad}, "pad", ""},
		{"raw files after a network error", "acme/pad", qmkFake{"v1/keyboards/acme/pad/info.json": "", "master/keyboards/acme/pad/info.json": pad}, "pad", ""},
		{"nowhere", "acme/none", qmkFake{}, "", "acme/none is not a file or a QMK keyboard"},
		{"a file that can't be read isn't fetched", filepath.Join(unreadable, "x.json"), nil, "", "permission denied"},
	}
	old := qmkClient
	t.Cleanup(func() { qmkClient = old })
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if os.Geteuid() == 0 && strings.Contains(tt.wantErr, "permission") {
				t.Skip("root reads anything")
			}
			qmkClient = &http.Client{Transport: tt.served}
			kb, err := readQMK(tt.src)
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
			if kb.Name != tt.want {
				t.Errorf("keyboard %q, want %q", kb.Name, tt.want)
			}
		})
	}
}

// board is a Keymapp-like keyboard for loadMap.
type board struct {
	recorder
	name string
	err  error
}

func (b *board) Keyboard() (string, error) { return b.name, b.err }
func (b *board) SetAll(rgb.Colour) error   { return nil }
func (b *board) Close() error              { return nil }

func TestLoadMap(t *testing.T) {
	mapFile := filepath.Join(t.TempDir(), "mine.txt")
	if err := os.WriteFile(mapFile, []byte("1 2 3\n4 5 6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	noKeyboard := errors.New("no keyboard connected to Keymapp")
	tests := []struct {
		name     string
		model    string
		mapFile  string
		kb       *board
		wantName string
		wantErr  string
	}{
		{"-map wins", "voyager", mapFile, &board{}, mapFile, ""},
		{"-model", "planck", "", &board{}, "planck", ""},
		{"detected", "", "", &board{name: "Voyager"}, "voyager", ""},
		{"-map missing", "", filepath.Join(t.TempDir(), "none.txt"), &board{}, "", "no such file"},
		{"unknown model", "nope", "", &board{}, "", "nope"},
		{"no built-in map", "", "", &board{name: "Mystery 9000"}, "", `no built-in map for "Mystery 9000"`},
		{"keyboard error", "", "", &board{err: noKeyboard}, "", noKeyboard.Error()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, file := tt.model, tt.mapFile
			c := common{model: &model, mapFile: &file}
			m, err := loadMap(c, tt.kb)
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
			if m.Name != tt.wantName {
				t.Errorf("map %q, want %q", m.Name, tt.wantName)
			}
		})
	}
}

func TestConnectUnknownBackend(t *testing.T) {
	backend, empty := "razer", ""
	_, err := conn{backend: &backend, socket: &empty, addr: &empty, device: &empty}.connect()
	if err == nil || !strings.Contains(err.Error(), "want keymapp or openrgb") {
		t.Errorf("err = %v", err)
	}
}
