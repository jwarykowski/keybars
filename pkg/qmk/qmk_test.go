package qmk

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, name string) Keyboard {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	kb, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	return kb
}

// tokens is a map's keys row by row, ignoring comments, spacing and "|".
func tokens(s string) string {
	var rows []string
	for _, line := range strings.Split(s, "\n") {
		line, _, _ = strings.Cut(line, "#")
		var f []string
		for _, t := range strings.Fields(line) {
			if t != "|" {
				f = append(f, t)
			}
		}
		if len(f) > 0 {
			rows = append(rows, strings.Join(f, " "))
		}
	}
	return strings.Join(rows, "\n")
}

func generate(t *testing.T, name string, o Options) string {
	t.Helper()
	g, err := load(t, name).Map(o)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := g.Format(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// The built-in maps must be what QMK generates; the Moonlander's was
// written by hand and checked on a real board.
func TestKeymappMatchesBuiltinMaps(t *testing.T) {
	for _, name := range []string{"moonlander", "voyager"} {
		want, err := os.ReadFile(filepath.Join("..", "..", "maps", name+".txt"))
		if err != nil {
			t.Fatal(err)
		}
		got := generate(t, name, Options{})
		if tokens(got) != tokens(string(want)) {
			t.Errorf("%s: generated\n%s\nbuilt-in\n%s", name, tokens(got), tokens(string(want)))
		}
	}
}

func TestFirmwareOrder(t *testing.T) {
	// The Moonlander's LEDs run down each column: the top row is every
	// fifth LED.
	got := tokens(generate(t, "moonlander", Options{Order: Firmware}))
	if top := strings.Split(got, "\n")[0]; top != "0 5 10 15 20 25 61 56 51 46 41 36" {
		t.Errorf("moonlander firmware top row = %q", top)
	}
	// The Voyager's LEDs are already in Keymapp's order, 0 included.
	v := tokens(generate(t, "voyager", Options{Order: Firmware}))
	if top := strings.Split(v, "\n")[0]; top != "0 1 2 3 4 5 26 27 28 29 30 31" {
		t.Errorf("voyager firmware top row = %q", top)
	}
}

func TestInnerColumn(t *testing.T) {
	got := tokens(generate(t, "moonlander", Options{Inner: true}))
	want := strings.Join([]string{
		"- 1 2 3 4 5 6 36 37 38 39 40 41 42",
		"7 8 9 10 11 12 13 43 44 45 46 47 48 49",
		"14 15 16 17 18 19 20 50 51 52 53 54 55 56",
		"21 22 23 24 25 26 - - 57 58 59 60 61 62",
		"27 28 29 30 31 - - - - 63 64 65 66 67",
	}, "\n")
	if got != want {
		t.Errorf("with inner columns:\n%s\nwant\n%s", got, want)
	}
}

func TestOnePieceBoard(t *testing.T) {
	kb, err := Parse(strings.NewReader(`{"keyboard_name":"pad","layouts":{"LAYOUT":{"layout":[
		{"matrix":[0,0],"x":0,"y":0},{"matrix":[0,1],"x":1,"y":0},
		{"matrix":[1,0],"x":0,"y":1},{"matrix":[1,1],"x":1,"y":1}]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	g, err := kb.Map(Options{})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	g.Format(&b)
	if got := b.String(); got != " -  1\n 2  3\n" {
		t.Errorf("one-piece map = %q", got)
	}
}

func TestFirmwareNeedsRGBMatrix(t *testing.T) {
	kb, _ := Parse(strings.NewReader(`{"keyboard_name":"pad","layouts":{"LAYOUT":{"layout":[{"matrix":[0,0],"x":0,"y":0}]}}}`))
	if _, err := kb.Map(Options{Order: Firmware}); err == nil {
		t.Error("firmware order without rgb_matrix: no error")
	}
}
