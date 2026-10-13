package bars

import (
	"slices"
	"strings"
	"testing"

	"github.com/jwarykowski/keybars/pkg/keymapp"
	"github.com/jwarykowski/keybars/pkg/layout"
)

// led is the LED under bar x at level in the test grid.
func led(g layout.Grid, x, level int) int {
	l, _ := g.LED(x, level)
	return l
}

// fake records what a renderer sends.
type fake struct {
	set map[int]keymapp.Colour
	all int
}

func (f *fake) Set(led int, c keymapp.Colour) error { f.set[led] = c; return nil }
func (f *fake) RestoreAll() error                   { f.all++; return nil }

var (
	green = keymapp.Colour{G: 255}
	red   = keymapp.Colour{R: 255}
	dark  = keymapp.Colour{}
)

// columns is the test grid's width: 12 bars, 2 rows.
const columns = 12

func setup(t *testing.T, reserve ...int) (*Renderer, *fake, layout.Grid) {
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
	return New(g, f, []keymapp.Colour{green, red}, reserve), f, g
}

func TestFrameColoursByLevel(t *testing.T) {
	r, f, g := setup(t)
	heights := make([]int, columns)
	heights[0] = 2
	heights[1] = 1
	if _, err := r.Frame(heights, nil); err != nil {
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
	r, f, _ := setup(t)
	heights := make([]int, columns)
	r.Frame(heights, nil)
	heights[3] = 1
	n, err := r.Frame(heights, nil)
	if err != nil || n != 1 {
		t.Fatalf("one bar up one level sent %d LEDs (%v), want 1", n, err)
	}
	if n, _ := r.Frame(heights, nil); n != 0 {
		t.Errorf("same frame again sent %d, want 0", n)
	}
	_ = f
}

func TestReserveIsNeverTouched(t *testing.T) {
	_, _, g := setup(t)
	keep := led(g, 2, 0)
	r, f, _ := setup(t, keep)
	heights := make([]int, columns)
	heights[2] = 2
	r.Frame(heights, nil)
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

func TestPeakDot(t *testing.T) {
	r, f, g := setup(t)
	r.WithPeaks(keymapp.Colour{B: 255})
	heights := make([]int, columns)
	peaks := make([]int, columns)
	peaks[4] = 2 // bar 4 empty, its peak on the top level
	r.Frame(heights, peaks)
	if f.set[led(g, 4, 1)] != (keymapp.Colour{B: 255}) || f.set[led(g, 4, 0)] != dark {
		t.Errorf("want a dot on top and dark below, got %v %v", f.set[led(g, 4, 1)], f.set[led(g, 4, 0)])
	}
	heights[4] = 2 // the bar covers its peak: bar colours win
	r.Frame(heights, peaks)
	if f.set[led(g, 4, 1)] != red {
		t.Errorf("bar over its peak: want red, got %v", f.set[led(g, 4, 1)])
	}
}

func TestInvalidateResends(t *testing.T) {
	r, f, _ := setup(t)
	heights := make([]int, columns)
	r.Frame(heights, nil)
	r.Invalidate()
	if n, _ := r.Frame(heights, nil); n != columns*2 {
		t.Errorf("after Invalidate sent %d, want the whole %d-LED grid", n, columns*2)
	}
	_ = f
}

func TestScale(t *testing.T) {
	// fine = 8 steps per key: 4 steps is half a key, so gain 1 rounds it up
	// to 1 and gain 2 makes it a whole key; nothing passes the height
	got := Scale([]int{0, 4, 12, 32}, 8, 1, 4)
	if want := []int{0, 1, 2, 4}; !slices.Equal(got, want) {
		t.Errorf("gain 1: %v, want %v", got, want)
	}
	got = Scale([]int{0, 4, 12, 32}, 8, 2, 4)
	if want := []int{0, 1, 3, 4}; !slices.Equal(got, want) {
		t.Errorf("gain 2: %v, want %v", got, want)
	}
}

// flushing is a backend that buffers Sets, as OpenRGB's does.
type flushing struct {
	fake
	flushes int
}

func (f *flushing) Flush() error { f.flushes++; return nil }

// A buffering backend is flushed once per frame that changed something.
func TestFrameFlushesOnce(t *testing.T) {
	m, err := layout.Parse("t", strings.NewReader("0 1 2\n3 4 5\n"))
	if err != nil {
		t.Fatal(err)
	}
	g, _ := layout.New(m, 2)
	f := &flushing{fake: fake{set: map[int]keymapp.Colour{}}}
	r := New(g, f, []keymapp.Colour{green}, nil)
	for i, want := range []int{1, 1, 2} {
		h := []int{2, 1, 0}
		if i == 2 {
			h = []int{0, 1, 0}
		}
		if _, err := r.Frame(h, nil); err != nil {
			t.Fatal(err)
		}
		if f.flushes != want {
			t.Errorf("frame %d: %d flushes, want %d", i, f.flushes, want)
		}
	}
}
