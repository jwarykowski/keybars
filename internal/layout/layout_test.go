package layout

import "testing"

func TestLED(t *testing.T) {
	for _, tc := range []struct {
		model    string
		x, level int
		want     int
	}{
		// Q and P sit on the second row: level 3 of the moonlander's 5 rows
		{"moonlander", 1, 3, 6},
		{"moonlander", 10, 3, 42},
		{"moonlander", 0, 0, 4},   // bottom-left
		{"moonlander", 11, 0, 40}, // bottom-right
		{"moonlander", 6, 4, 61},  // the 6 key
		{"voyager", 1, 2, 7},      // Q
		{"voyager", 6, 2, 32},     // Y
		{"voyager", 11, 0, 49},    // bottom-right
	} {
		g, err := New(tc.model, 0)
		if err != nil {
			t.Fatal(err)
		}
		g.Height = g.Model.Rows
		if got := g.LED(tc.x, tc.level); got != tc.want {
			t.Errorf("%s LED(%d, %d) = %d, want %d", tc.model, tc.x, tc.level, got, tc.want)
		}
	}
}

func TestNew(t *testing.T) {
	g, err := New("moonlander", 0)
	if err != nil || g.Height != 4 {
		t.Fatalf("default height = %d, %v; want 4 (all but the number row)", g.Height, err)
	}
	if _, err := New("voyager", 5); err == nil {
		t.Error("voyager height 5: want an error, it has 4 rows")
	}
	if _, err := New("planck", 0); err == nil {
		t.Error("unknown model: want an error")
	}
}
