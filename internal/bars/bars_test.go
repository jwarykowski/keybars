package bars

import (
	"testing"

	"github.com/jwarykowski/keybars/internal/keymapp"
	"github.com/jwarykowski/keybars/internal/layout"
)

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

func setup(t *testing.T, off Off, reserve ...int) (*Renderer, *fake, layout.Grid) {
	t.Helper()
	g, err := layout.New("moonlander", 2)
	if err != nil {
		t.Fatal(err)
	}
	f := &fake{set: map[int]keymapp.Colour{}}
	return New(g, f, []keymapp.Colour{green, red}, off, reserve), f, g
}

func TestFrameColoursByLevel(t *testing.T) {
	r, f, g := setup(t, Dark)
	heights := make([]int, layout.Columns)
	heights[0] = 2
	heights[1] = 1
	if _, err := r.Frame(heights); err != nil {
		t.Fatal(err)
	}
	if f.set[g.LED(0, 0)] != green || f.set[g.LED(0, 1)] != red {
		t.Errorf("bar 0: want green below red, got %v %v", f.set[g.LED(0, 0)], f.set[g.LED(0, 1)])
	}
	if f.set[g.LED(1, 0)] != green || f.set[g.LED(1, 1)] != dark {
		t.Errorf("bar 1: want green then dark, got %v %v", f.set[g.LED(1, 0)], f.set[g.LED(1, 1)])
	}
	if len(f.set) != layout.Columns*2 {
		t.Errorf("first frame set %d LEDs, want the whole %d-LED grid", len(f.set), layout.Columns*2)
	}
}

func TestFrameSendsOnlyChanges(t *testing.T) {
	r, f, _ := setup(t, Dark)
	heights := make([]int, layout.Columns)
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
	heights := make([]int, layout.Columns)
	heights[5] = 1
	r.Frame(heights)
	if len(f.set) != 1 {
		t.Fatalf("set %d LEDs, want only the lit one", len(f.set))
	}
	heights[5] = 0
	r.Frame(heights)
	if len(f.restored) != 1 || f.restored[0] != g.LED(5, 0) {
		t.Errorf("restored %v, want [%d]", f.restored, g.LED(5, 0))
	}
}

func TestReserveIsNeverTouched(t *testing.T) {
	_, _, g := setup(t, Dark)
	keep := g.LED(2, 0)
	r, f, _ := setup(t, Dark, keep)
	heights := make([]int, layout.Columns)
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
