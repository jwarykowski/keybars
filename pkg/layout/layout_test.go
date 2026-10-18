package layout

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// the repo's built-in maps
var builtin = os.DirFS("../../maps")

func TestParse(t *testing.T) {
	m, err := Parse("t", strings.NewReader(`
# a comment
 1  2 | 3   # trailing comment
 4  - | 5  6
`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Rows() != 2 || m.Columns() != 4 {
		t.Fatalf("got %dx%d, want 2 rows x 4 columns (the widest row)", m.Rows(), m.Columns())
	}
	g, _ := New(m, 2)
	if led, ok := g.LED(0, 0); !ok || led != 4 {
		t.Errorf("bottom-left = %d %v, want 4", led, ok)
	}
	if _, ok := g.LED(1, 0); ok {
		t.Error("- should be a gap")
	}
	if _, ok := g.LED(3, 1); ok {
		t.Error("a short row should be padded with gaps")
	}
	if !slices.Equal(m.LEDs(), []int{1, 2, 3, 4, 5, 6}) {
		t.Errorf("LEDs = %v", m.LEDs())
	}
}

func TestParseErrors(t *testing.T) {
	for _, in := range []string{"", "# only a comment\n", "1 x 3\n", "1 -2\n"} {
		if _, err := Parse("t", strings.NewReader(in)); err == nil {
			t.Errorf("Parse(%q): want an error", in)
		}
	}
}

func TestBuiltinMaps(t *testing.T) {
	if got := Builtin(builtin); !slices.Equal(got, []string{"moonlander", "planck", "voyager"}) {
		t.Fatalf("Builtin = %v", got)
	}
	for _, tc := range []struct {
		model    string
		x, level int
		want     int
	}{
		// Q and P on the second row, in Keymapp's key order
		{"moonlander", 1, 3, 8},
		{"moonlander", 10, 3, 48},
		{"moonlander", 6, 4, 37},  // the 6 key
		{"moonlander", 11, 4, 42}, // top-right, as photographed
		{"moonlander", 11, 0, 67}, // bottom-right, as photographed
		{"voyager", 1, 2, 7},      // Q
		{"voyager", 6, 2, 32},     // Y
		{"voyager", 11, 3, 31},
	} {
		m, err := Load(builtin, tc.model)
		if err != nil {
			t.Fatal(err)
		}
		g, _ := New(m, m.Rows())
		if got, _ := g.LED(tc.x, tc.level); got != tc.want {
			t.Errorf("%s LED(%d, %d) = %d, want %d", tc.model, tc.x, tc.level, got, tc.want)
		}
	}
}

func TestNew(t *testing.T) {
	m, _ := Load(builtin, "moonlander")
	g, err := New(m, 0)
	if err != nil || g.Height != 4 {
		t.Fatalf("default height = %d, %v; want 4 (all but the number row)", g.Height, err)
	}
	if _, err := New(m, 6); err == nil {
		t.Error("height 6 on a 5-row map: want an error")
	}
	if _, err := Load(builtin, "ergodox"); err == nil {
		t.Error("unknown model: want an error")
	}
}

func TestDetect(t *testing.T) {
	for kb, want := range map[string]string{"Moonlander MK1": "moonlander", "Voyager": "voyager", "Planck EZ Glow": "planck", "ErgoDox EZ": ""} {
		got, ok := Detect(builtin, kb)
		if got != want || ok != (want != "") {
			t.Errorf("Detect(%q) = %q %v, want %q", kb, got, ok, want)
		}
	}
}

func TestFromCells(t *testing.T) {
	m, err := FromCells("pad", [][]int{{0, 1, -1}, {}, {2, 3}})
	if err != nil {
		t.Fatal(err)
	}
	if m.Rows() != 2 || m.Columns() != 3 {
		t.Fatalf("%d rows x %d columns, want 2 x 3", m.Rows(), m.Columns())
	}
	if got := m.LEDs(); len(got) != 4 || got[3] != 3 {
		t.Errorf("LEDs = %v", got)
	}
	if _, err := FromCells("empty", nil); err == nil {
		t.Error("no rows: no error")
	}
}
