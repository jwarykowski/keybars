// Package qmk writes keybars maps from QMK keyboard definitions
// (keyboards/<keyboard>/keyboard.json in qmk_firmware), so a board needs no
// `keybars find` session when QMK already knows where its keys are.
//
// QMK gives each key's matrix position and physical x/y, and each RGB LED's
// matrix position. From that, a map can be drawn in either numbering:
//
//   - Keymapp: what Keymapp's API takes. Each half is numbered on its own,
//     row by row in QMK's layout order, with the thumb cluster last; the left
//     half first, then the right. Keymapp never lights index 0, so it is a
//     gap.
//   - Firmware: the LED's index in QMK's rgb_matrix layout, for backends that
//     talk to the firmware directly. On the Moonlander this is column by
//     column, unlike Keymapp's.
//
// The map's grid comes from physical positions: keys sharing an x form a
// column, ordered top to bottom. On a split board the thumb cluster (each
// half's last matrix row) and a short inner column are left out, as in the
// hand-made maps.
package qmk

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
	"strings"
)

// Order is how keys are numbered in the map.
type Order int

// The orders a map can number keys in: Keymapp's, or the firmware's LEDs.
const (
	Keymapp Order = iota
	Firmware
)

// ParseOrder reads "keymapp" or "firmware".
func ParseOrder(s string) (Order, error) {
	switch s {
	case "keymapp":
		return Keymapp, nil
	case "firmware":
		return Firmware, nil
	}
	return 0, fmt.Errorf("order %q: want keymapp or firmware", s)
}

// Keyboard is the part of a QMK keyboard.json that maps need.
type Keyboard struct {
	Name      string            `json:"keyboard_name"`
	Layouts   map[string]Layout `json:"layouts"`
	RGBMatrix struct {
		Layout []LED `json:"layout"`
	} `json:"rgb_matrix"`
}

// Layout is one of a keyboard's physical layouts, keys in QMK's order.
type Layout struct {
	Keys []Key `json:"layout"`
}

// Key is a key's matrix position and its top-left corner, in key units.
type Key struct {
	Matrix [2]int  `json:"matrix"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

// LED is an rgb_matrix entry; Matrix is nil for underglow and other LEDs
// under no key.
type LED struct {
	Matrix *[2]int `json:"matrix"`
}

// Parse reads a keyboard.json.
func Parse(r io.Reader) (Keyboard, error) {
	var kb Keyboard
	if err := json.NewDecoder(r).Decode(&kb); err != nil {
		return Keyboard{}, fmt.Errorf("keyboard.json: %w", err)
	}
	if len(kb.Layouts) == 0 {
		return Keyboard{}, errors.New("keyboard.json has no layouts (it may inherit them from a parent info.json)")
	}
	return kb, nil
}

// Options shape the generated map.
type Options struct {
	Order  Order
	Layout string // QMK layout name; default LAYOUT, else the first by name
	Inner  bool   // keep a split half's short inner column
}

// Grid is a generated map: Cells[row][column], top row first, -1 for a
// gap. Split is the number of left-half columns, 0 for a one-piece board.
type Grid struct {
	Cells [][]int
	Split int
}

// Map draws the keyboard as a grid of key indices.
func (kb Keyboard) Map(o Options) (Grid, error) {
	keys, err := kb.layout(o.Layout)
	if err != nil {
		return Grid{}, err
	}
	halves := split(keys)

	index := map[[2]int]int{}
	switch o.Order {
	case Keymapp:
		n := 0
		for _, h := range halves {
			main, thumbs := partition(h, len(halves) > 1)
			for _, k := range append(main, thumbs...) {
				index[k.Matrix] = n
				n++
			}
		}
	case Firmware:
		for i, l := range kb.RGBMatrix.Layout {
			if l.Matrix != nil {
				index[*l.Matrix] = i
			}
		}
		if len(index) == 0 {
			return Grid{}, fmt.Errorf("%s has no rgb_matrix layout", kb.Name)
		}
	}

	var g Grid
	var cols [][]Key
	for i, h := range halves {
		main, _ := partition(h, len(halves) > 1)
		c := columns(main)
		if len(halves) > 1 && !o.Inner {
			c = dropInner(c, i == 0)
		}
		cols = append(cols, c...)
		if i == 0 && len(halves) > 1 {
			g.Split = len(c)
		}
	}
	rows := 0
	for _, c := range cols {
		rows = max(rows, len(c))
	}
	g.Cells = make([][]int, rows)
	for r := range g.Cells {
		g.Cells[r] = make([]int, len(cols))
		for x, c := range cols {
			g.Cells[r][x] = -1
			if r >= len(c) {
				continue
			}
			led, ok := index[c[r].Matrix]
			if ok && (o.Order != Keymapp || led != 0) {
				g.Cells[r][x] = led
			}
		}
	}
	return g, nil
}

func (kb Keyboard) layout(name string) ([]Key, error) {
	if name == "" {
		name = "LAYOUT"
		if _, ok := kb.Layouts[name]; !ok {
			names := make([]string, 0, len(kb.Layouts))
			for n := range kb.Layouts {
				names = append(names, n)
			}
			sort.Strings(names)
			name = names[0]
		}
	}
	l, ok := kb.Layouts[name]
	if !ok {
		return nil, fmt.Errorf("%s has no layout %q", kb.Name, name)
	}
	return l.Keys, nil
}

// split separates the halves of a split board at the widest horizontal gap
// between keys, if any is two key widths or more; a one-piece board is one
// half. Each half keeps QMK's key order.
func split(keys []Key) [][]Key {
	xs := make([]float64, len(keys))
	for i, k := range keys {
		xs[i] = k.X
	}
	slices.Sort(xs)
	at, widest := 0.0, 0.0
	for i := 1; i < len(xs); i++ {
		if gap := xs[i] - xs[i-1]; gap > widest {
			at, widest = xs[i], gap
		}
	}
	if widest < 2 {
		return [][]Key{keys}
	}
	var left, right []Key
	for _, k := range keys {
		if k.X < at {
			left = append(left, k)
		} else {
			right = append(right, k)
		}
	}
	return [][]Key{left, right}
}

// partition splits a half into its main keys and, on a split board, its
// thumb cluster: the keys on the half's last matrix row.
func partition(half []Key, isSplit bool) (main, thumbs []Key) {
	if !isSplit {
		return half, nil
	}
	last := 0
	for _, k := range half {
		last = max(last, k.Matrix[0])
	}
	for _, k := range half {
		if k.Matrix[0] == last {
			thumbs = append(thumbs, k)
		} else {
			main = append(main, k)
		}
	}
	return main, thumbs
}

// columns groups keys by x, left to right, each column top to bottom.
func columns(keys []Key) [][]Key {
	byX := map[float64][]Key{}
	for _, k := range keys {
		x := math.Round(k.X*4) / 4
		byX[x] = append(byX[x], k)
	}
	xs := make([]float64, 0, len(byX))
	for x := range byX {
		xs = append(xs, x)
	}
	slices.Sort(xs)
	out := make([][]Key, len(xs))
	for i, x := range xs {
		c := byX[x]
		sort.SliceStable(c, func(a, b int) bool { return c[a].Y < c[b].Y })
		out[i] = c
	}
	return out
}

// dropInner leaves out the column beside the split (the left half's last,
// the right half's first) when it is shorter than its neighbour, like the
// Moonlander's three-key inner columns.
func dropInner(cols [][]Key, left bool) [][]Key {
	if len(cols) < 2 {
		return cols
	}
	if left {
		if len(cols[len(cols)-1]) < len(cols[len(cols)-2]) {
			return cols[:len(cols)-1]
		}
		return cols
	}
	if len(cols[0]) < len(cols[1]) {
		return cols[1:]
	}
	return cols
}

// Format writes the grid as a keybars map, header lines first as comments.
func (g Grid) Format(w io.Writer, header ...string) error {
	var b strings.Builder
	for _, h := range header {
		b.WriteString("# " + h + "\n")
	}
	for _, row := range g.Cells {
		var f []string
		for x, led := range row {
			if g.Split > 0 && x == g.Split {
				f = append(f, "|")
			}
			if led < 0 {
				f = append(f, " -")
			} else {
				f = append(f, fmt.Sprintf("%2d", led))
			}
		}
		b.WriteString(strings.Join(f, " ") + "\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}
