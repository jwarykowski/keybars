package maps

import (
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
