// Package bars turns bar heights into LED colours and sends only what changed
// since the last frame: at about a millisecond per LED, a full repaint of a
// 60-key grid would cap the frame rate near 15 fps, while a typical frame
// changes far fewer keys.
package bars

import (
	"fmt"
	"strings"

	"github.com/jwarykowski/keybars/internal/keymapp"
	"github.com/jwarykowski/keybars/internal/layout"
	"github.com/jwarykowski/keybars/internal/rgb"
)

// Renderer paints frames onto a keyboard.
type Renderer struct {
	grid    layout.Grid
	leds    keymapp.LEDs
	colours []keymapp.Colour // one per level, bottom first
	reserve map[int]bool
	shown   map[int]keymapp.Colour // what each LED shows now; absent = layout
	peak    *keymapp.Colour        // peak dot colour; nil = no dots
}

// WithPeaks draws a dot of colour c at each bar's peak level (see Peaks).
func (r *Renderer) WithPeaks(c keymapp.Colour) *Renderer {
	r.peak = &c
	return r
}

// New returns a renderer. colours run bottom to top and are stretched over
// the grid's height; reserved LEDs are never touched. Unlit keys are dark:
// once Keymapp drives any key, the firmware shows only what the API sets.
func New(grid layout.Grid, leds keymapp.LEDs, colours []keymapp.Colour, reserve []int) *Renderer {
	r := &Renderer{grid: grid, leds: leds, reserve: map[int]bool{}, shown: map[int]keymapp.Colour{}}
	for _, l := range reserve {
		r.reserve[l] = true
	}
	for level := 0; level < grid.Height; level++ {
		i := 0
		if grid.Height > 1 {
			i = level * (len(colours) - 1) / (grid.Height - 1)
		}
		r.colours = append(r.colours, colours[i])
	}
	return r
}

// Target is what every LED in the grid should show for heights (one per
// bar, 0..grid height) and peaks (nil without dots); absent means the
// layout's colour.
func (r *Renderer) Target(heights, peaks []int) map[int]keymapp.Colour {
	t := map[int]keymapp.Colour{}
	for x := 0; x < r.grid.Columns() && x < len(heights); x++ {
		for level := 0; level < r.grid.Height; level++ {
			led, ok := r.grid.LED(x, level)
			if !ok || r.reserve[led] {
				continue
			}
			switch {
			case heights[x] > level:
				t[led] = r.colours[level]
			case r.peak != nil && x < len(peaks) && peaks[x] == level+1:
				t[led] = *r.peak
			default:
				t[led] = keymapp.Colour{}
			}
		}
	}
	return t
}

// Frame paints heights and peak dots (nil for none), sending only the LEDs
// that changed. It returns how many it sent.
func (r *Renderer) Frame(heights, peaks []int) (int, error) {
	target := r.Target(heights, peaks)
	sent := 0
	for led, c := range target {
		if old, ok := r.shown[led]; ok && old == c {
			continue
		}
		if err := r.leds.Set(led, c); err != nil {
			return sent, err
		}
		r.shown[led] = c
		sent++
	}
	if sent > 0 {
		if err := rgb.Flush(r.leds); err != nil {
			return sent, err
		}
	}
	return sent, nil
}

// Invalidate forgets what the keyboard shows, so the next frame resends
// every LED: Keymapp drops API colours when anything restores the layout
// (another tool, a kontroll --sustain timer), and nothing tells us.
func (r *Renderer) Invalidate() {
	r.shown = map[int]keymapp.Colour{}
}

// Clear hands the board back to the layout.
func (r *Renderer) Clear() error {
	r.shown = map[int]keymapp.Colour{}
	return r.leds.RestoreAll()
}

// ParseColours reads a comma-separated list of #rrggbb colours.
func ParseColours(s string) ([]keymapp.Colour, error) {
	var out []keymapp.Colour
	for _, part := range strings.Split(s, ",") {
		var c keymapp.Colour
		p := strings.TrimPrefix(strings.TrimSpace(part), "#")
		if len(p) != 6 {
			return nil, fmt.Errorf("colour %q: want #rrggbb", part)
		}
		if _, err := fmt.Sscanf(p, "%02x%02x%02x", &c.R, &c.G, &c.B); err != nil {
			return nil, fmt.Errorf("colour %q: %w", part, err)
		}
		out = append(out, c)
	}
	return out, nil
}

// Scale turns fine-grained heights (fine steps per key) into key levels,
// amplified by gain, rounded, and capped at height.
func Scale(heights []int, fine int, gain float64, height int) []int {
	out := make([]int, len(heights))
	for i, h := range heights {
		out[i] = min(height, int(float64(h)*gain/float64(fine)+0.5))
	}
	return out
}
