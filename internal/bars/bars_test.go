package bars

import (
	"strings"
	"testing"

	"github.com/jwarykowski/keybars/internal/keymapp"
	"github.com/jwarykowski/keybars/internal/layout"
)

// led is the LED under bar x at level in the test grid.
func led(g layout.Grid, x, level int) int {
	l, _ := g.LED(x, level)
	return l
}

// fake records what a renderer sends.
type fake struct {
	set      map[int]keymapp.Colour
	restored []int
	all      int
}

func (f *fake) Set(led int, c keymapp.Colour) error { f.set[led] = c; return nil }
func (f *fake) Restore(led int) error               { f.restored = append(f.restored, led); return nil }
func (f *fake) RestoreAll() error                   { f.all++; return nil }

var (
	green = keymapp.Colour{G: 255}
	red   = keymapp.Colour{R: 255}
	dark  = keymapp.Colour{}
)

// columns is the test grid's width: 12 bars, 2 rows.
const columns = 12

func setup(t *testing.T, off Off, reserve ...int) (*Renderer, *fake, layout.Grid) {
	t.Helper()
	m, err := layout.Parse("t", strings.NewReader(`
 0  1  2  3  4  5  6  7  8  9 10 11
12 13 14 15 16 17 18 19 20 21 22 23
`))
	if err != nil {
		t.Fatal(err)
	}
	g, err := layout.New(m, 2)
	if err != nil {
		t.Fatal(err)
	}
	f := &fake{set: map[int]keymapp.Colour{}}
	return New(g, f, []keymapp.Colour{green, red}, off, reserve), f, g
}

func TestFrameColoursByLevel(t *testing.T) {
	r, f, g := setup(t, Dark)
	heights := make([]int, columns)
	heights[0] = 2
	heights[1] = 1
	if _, err := r.Frame(heights); err != nil {
		t.Fatal(err)
	}
	if f.set[led(g, 0, 0)] != green || f.set[led(g, 0, 1)] != red {
		t.Errorf("bar 0: want green below red, got %v %v", f.set[led(g, 0, 0)], f.set[led(g, 0, 1)])
	}
	if f.set[led(g, 1, 0)] != green || f.set[led(g, 1, 1)] != dark {
		t.Errorf("bar 1: want green then dark, got %v %v", f.set[led(g, 1, 0)], f.set[led(g, 1, 1)])
	}
	if len(f.set) != columns*2 {
		t.Errorf("first frame set %d LEDs, want the whole %d-LED grid", len(f.set), columns*2)
	}
}

func TestFrameSendsOnlyChanges(t *testing.T) {
	r, f, _ := setup(t, Dark)
	heights := make([]int, columns)
	r.Frame(heights)
	heights[3] = 1
	n, err := r.Frame(heights)
	if err != nil || n != 1 {
		t.Fatalf("one bar up one level sent %d LEDs (%v), want 1", n, err)
	}
	if n, _ := r.Frame(heights); n != 0 {
		t.Errorf("same frame again sent %d, want 0", n)
	}
	_ = f
}

func TestLayoutOffRestoresKeys(t *testing.T) {
	r, f, g := setup(t, Layout)
	heights := make([]int, columns)
	heights[5] = 1
	r.Frame(heights)
	if len(f.set) != 1 {
		t.Fatalf("set %d LEDs, want only the lit one", len(f.set))
	}
	heights[5] = 0
	r.Frame(heights)
	if len(f.restored) != 1 || f.restored[0] != led(g, 5, 0) {
		t.Errorf("restored %v, want [%d]", f.restored, led(g, 5, 0))
	}
}

func TestReserveIsNeverTouched(t *testing.T) {
	_, _, g := setup(t, Dark)
	keep := led(g, 2, 0)
	r, f, _ := setup(t, Dark, keep)
	heights := make([]int, columns)
	heights[2] = 2
	r.Frame(heights)
	if _, ok := f.set[keep]; ok {
		t.Errorf("reserved LED %d was set", keep)
	}
}

func TestParseColours(t *testing.T) {
	cs, err := ParseColours("#00ff00, ff0000")
	if err != nil || len(cs) != 2 || cs[0] != green || cs[1] != red {
		t.Fatalf("got %v, %v", cs, err)
	}
	if _, err := ParseColours("#fff"); err == nil {
		t.Error("short colour: want an error")
	}
}
