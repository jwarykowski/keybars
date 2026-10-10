// Package layout maps a bar grid onto a keyboard's LED indices (ZSA's QMK
// rgb_matrix order, the indices Keymapp's API takes).
//
// The grid is 12 columns, the six main columns of each half from left to
// right, and up to five levels counted from the bottom row up. The inner
// columns and thumb clusters are left out: they don't line up with the rest.
package layout

import (
	"fmt"
	"sort"
)

// Columns is the number of bars: six main columns per half.
const Columns = 12

// Model is a keyboard: how a (column, physical row) pair maps to an LED,
// and its physical rows from top (0, the number row) down.
type Model struct {
	Name string
	Rows int
	led  func(x, row int) int
}

var models = map[string]Model{
	// left half numbered by column (5 LEDs each), right half mirrored from
	// LED 36 with column 0 on the outside
	"moonlander": {Name: "moonlander", Rows: 5, led: func(x, row int) int {
		if x < 6 {
			return x*5 + row
		}
		return 36 + (11-x)*5 + row
	}},
	// numbered by row, 6 LEDs per row; the right half starts at LED 26
	"voyager": {Name: "voyager", Rows: 4, led: func(x, row int) int {
		if x < 6 {
			return row*6 + x
		}
		return 26 + row*6 + (x - 6)
	}},
}

// Models lists the supported model names.
func Models() []string {
	names := make([]string, 0, len(models))
	for n := range models {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Grid is the bar area: Height levels of Columns bars.
type Grid struct {
	Model  Model
	Height int
}

// New returns the grid for a model, height levels tall from the bottom row.
// height 0 uses every row but the number row, which is left for other
// lights (agent status, say).
func New(model string, height int) (Grid, error) {
	m, ok := models[model]
	if !ok {
		return Grid{}, fmt.Errorf("unknown model %q (have %v)", model, Models())
	}
	if height == 0 {
		height = m.Rows - 1
	}
	if height < 1 || height > m.Rows {
		return Grid{}, fmt.Errorf("height %d: %s has %d rows", height, model, m.Rows)
	}
	return Grid{Model: m, Height: height}, nil
}

// LED is the index of bar x at level (0 = bottom row).
func (g Grid) LED(x, level int) int {
	return g.Model.led(x, g.Model.Rows-1-level)
}
