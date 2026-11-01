package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	builtin "github.com/jwarykowski/keybars/maps"
	"github.com/jwarykowski/keybars/pkg/layout"
	"github.com/jwarykowski/keybars/pkg/openrgb"
	"github.com/jwarykowski/keybars/pkg/rgb"
)

// TestLiveOpenRGB drives a real OpenRGB server's debug keyboards, set up by
// scripts/openrgb-check.sh, and reads back which keys it lit.
func TestLiveOpenRGB(t *testing.T) {
	addr := os.Getenv("KEYBARS_OPENRGB")
	if addr == "" {
		t.Skip("KEYBARS_OPENRGB not set: scripts/openrgb-check.sh runs this against OpenRGB")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, device := range []string{"Debug ANSI", "Debug ISO", "Debug JIS", "Corne", "Sofle"} {
		t.Run(device, func(t *testing.T) {
			before := describe(t, addr, device)
			grid := liveGrid(t, before)
			heights := make([]int, grid.Columns())
			for x := range heights {
				heights[x] = x % (grid.Height + 1)
			}

			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			old := os.Stdin
			os.Stdin = r
			t.Cleanup(func() { os.Stdin = old })
			go func() {
				done <- run([]string{"-backend", "openrgb", "-openrgb", addr, "-device", device, "-stdin", "-peaks=false", "-idle", "1h"})
			}()
			fmt.Fprintln(w, strings.Trim(strings.Join(strings.Fields(fmt.Sprint(heights)), ";"), "[]"))

			var lit map[int]bool
			want := wantLit(grid, heights)
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
				if lit = litLEDs(describe(t, addr, device)); equal(lit, want) {
					break
				}
			}
			w.Close()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if !equal(lit, want) {
				t.Errorf("lit %v\nwant %v", keys(lit), keys(want))
			}
			if after := describe(t, addr, device); after.ActiveMode != before.ActiveMode {
				t.Errorf("mode %d after exit, want %d back", after.ActiveMode, before.ActiveMode)
			}
		})
	}
}

// describe is device as the server describes it now.
func describe(t *testing.T, addr, device string) openrgb.Controller {
	t.Helper()
	c, err := openrgb.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctls, err := c.Controllers()
	if err != nil {
		t.Fatal(err)
	}
	for _, ctl := range ctls {
		if ctl.Name == device {
			return ctl
		}
	}
	t.Fatalf("OpenRGB has no %q", device)
	return openrgb.Controller{}
}

// liveGrid is the grid keybars should use for ctl: QMK's map for a board it
// knows, else its key matrix.
func liveGrid(t *testing.T, ctl openrgb.Controller) layout.Grid {
	t.Helper()
	var m layout.Map
	var err error
	if b, ok := builtin.MatchQMK(ctl.Name); ok && b.LEDs == len(ctl.LEDs) {
		m, err = layout.Load(builtin.QMK, b.ID)
	} else if cells, ok := ctl.Map(); ok {
		m, err = layout.FromCells(ctl.Name, cells)
	} else {
		t.Fatalf("no map for %s", ctl.Name)
	}
	if err != nil {
		t.Fatal(err)
	}
	g, err := layout.New(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func wantLit(g layout.Grid, heights []int) map[int]bool {
	want := map[int]bool{}
	for x, h := range heights {
		for level := range h {
			if led, ok := g.LED(x, level); ok {
				want[led] = true
			}
		}
	}
	return want
}

func litLEDs(ctl openrgb.Controller) map[int]bool {
	lit := map[int]bool{}
	for i, c := range ctl.Colours {
		if c != (rgb.Colour{}) {
			lit[i] = true
		}
	}
	return lit
}

func equal(a, b map[int]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func keys(m map[int]bool) []int {
	var out []int
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}
