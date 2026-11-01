package maps

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/jwarykowski/keybars/pkg/layout"
)

func TestEveryQMKBoardHasAMap(t *testing.T) {
	boards := Boards()
	if len(boards) < 20 {
		t.Fatalf("%d boards", len(boards))
	}
	seen := map[string]bool{}
	for _, b := range boards {
		if seen[b.ID] {
			t.Errorf("%s listed twice", b.ID)
		}
		seen[b.ID] = true
		m, err := layout.Load(QMK, b.ID)
		if err != nil {
			t.Errorf("%s: %v", b.ID, err)
			continue
		}
		if m.Rows() < 3 {
			t.Errorf("%s: %d rows", b.ID, m.Rows())
		}
		if leds := m.LEDs(); b.LEDs == 0 || slices.Max(leds) >= b.LEDs {
			t.Errorf("%s: map reaches LED %d of %d", b.ID, slices.Max(leds), b.LEDs)
		}
	}
}

func TestMatchQMKIsExact(t *testing.T) {
	for name, want := range map[string]string{
		"Keychron Q1":     "keychron-q1",
		"keychron q1":     "keychron-q1",
		"GMMK Pro":        "gmmk-pro",
		"CTRL":            "drop-ctrl",
		"crkbd":           "corne",
		"Keychron Q1 Pro": "",
		"Q10":             "",
		"":                "",
	} {
		b, ok := MatchQMK(name)
		if ok != (want != "") || b.ID != want {
			t.Errorf("MatchQMK(%q) = %q %v, want %q", name, b.ID, ok, want)
		}
	}
}

func TestQMKMapsAreNotZSAModels(t *testing.T) {
	for _, m := range layout.Builtin(FS) {
		if _, ok := MatchQMK(m); ok {
			t.Errorf("%s is in both", m)
		}
	}
	if got := layout.Builtin(FS); len(got) != 3 {
		t.Errorf("ZSA models: %v", got)
	}
}

// TestBarsStandStraight checks each built-in map against its board's shape
// from QMK: the keys in one bar sit within a key's width of each other, bottom
// to top, so a bar is a column on the board, not a diagonal.
func TestBarsStandStraight(t *testing.T) {
	b, err := os.ReadFile("shapes.json")
	if err != nil {
		t.Fatal(err)
	}
	var shapes []struct {
		ID   string `json:"id"`
		Keys []struct {
			X, Y, W  float64
			Row, Col int
			LED      int `json:"led"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(b, &shapes); err != nil {
		t.Fatal(err)
	}
	if len(shapes) != len(Boards())+len(layout.Builtin(FS)) {
		t.Errorf("%d shapes for %d maps", len(shapes), len(Boards())+len(layout.Builtin(FS)))
	}
	for _, s := range shapes {
		type key struct {
			row  int
			x, y float64
		}
		cols := map[int][]key{}
		for _, k := range s.Keys {
			if k.LED >= 0 && k.Row >= 1 { // the bars leave the top row
				cols[k.Col] = append(cols[k.Col], key{k.Row, k.X + k.W/2, k.Y})
			}
		}
		for c, keys := range cols {
			slices.SortFunc(keys, func(a, b key) int { return a.row - b.row })
			lo, hi := keys[0].x, keys[0].x
			for i, k := range keys {
				lo, hi = min(lo, k.x), max(hi, k.x)
				if i > 0 && k.y <= keys[i-1].y {
					t.Errorf("%s: column %d, row %d isn't below row %d", s.ID, c, k.row, keys[i-1].row)
				}
			}
			if hi-lo >= 1 {
				t.Errorf("%s: column %d leans %.2f keys", s.ID, c, hi-lo)
			}
		}
	}
}
