// Package layout reads keyboard maps: Keymapp's index for each key, drawn as
// a text grid. Keymapp numbers each half on its own, row by row, not in the
// firmware's LED order.
//
//	# moonlander: rows top to bottom, one index per key
//	 0  1  2  3  4  5 | 37 38 39 40 41 42
//	 7  8  9 10 11 12 | 44 45 46 47 48 49
//
// Each line is a row of keys, top to bottom; numbers are key indices, "-"
// marks a gap, "|" is decoration (between halves, say), "#" starts a
// comment. Columns become bars and rows become levels, so the grid can be
// any size; rows may be ragged.
package layout

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Map is a parsed keyboard map.
type Map struct {
	Name  string
	cells [][]int // [row][column], top row first; -1 = no key
}

// Parse reads a map; name labels errors and the map.
func Parse(name string, r io.Reader) (Map, error) {
	m := Map{Name: name}
	cols := 0
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line, _, _ := strings.Cut(sc.Text(), "#")
		var row []int
		for f := range strings.FieldsSeq(line) {
			switch f {
			case "|":
				continue
			case "-":
				row = append(row, -1)
			default:
				led, err := strconv.Atoi(f)
				if err != nil || led < 0 {
					return Map{}, fmt.Errorf("%s:%d: %q is not an LED index or -", name, n, f)
				}
				row = append(row, led)
			}
		}
		if len(row) == 0 {
			continue
		}
		cols = max(cols, len(row))
		m.cells = append(m.cells, row)
	}
	if err := sc.Err(); err != nil {
		return Map{}, err
	}
	if len(m.cells) == 0 {
		return Map{}, fmt.Errorf("%s: no rows", name)
	}
	for i, row := range m.cells {
		for len(row) < cols {
			row = append(row, -1)
		}
		m.cells[i] = row
	}
	return m, nil
}

// FromCells makes a map from a grid of key indices, top row first, -1 for
// a gap. Rows may be ragged; empty rows are dropped.
func FromCells(name string, cells [][]int) (Map, error) {
	var b strings.Builder
	for _, row := range cells {
		for _, led := range row {
			if led < 0 {
				b.WriteString(" -")
			} else {
				b.WriteString(" " + strconv.Itoa(led))
			}
		}
		b.WriteString("\n")
	}
	return Parse(name, strings.NewReader(b.String()))
}

// Rows is the number of rows of keys.
func (m Map) Rows() int { return len(m.cells) }

// Columns is the number of columns, the widest row.
func (m Map) Columns() int { return len(m.cells[0]) }

// LEDs lists every LED in the map.
func (m Map) LEDs() []int {
	var out []int
	for _, row := range m.cells {
		for _, led := range row {
			if led >= 0 {
				out = append(out, led)
			}
		}
	}
	return out
}

// Builtin lists the maps in fsys (files named <model>.txt).
func Builtin(fsys fs.FS) []string {
	names, _ := fs.Glob(fsys, "*.txt")
	for i, n := range names {
		names[i] = strings.TrimSuffix(n, ".txt")
	}
	sort.Strings(names)
	return names
}

// Load reads the built-in map for a model.
func Load(fsys fs.FS, model string) (Map, error) {
	f, err := fsys.Open(model + ".txt")
	if err != nil {
		return Map{}, fmt.Errorf("no map for %q (have %v; or pass -map FILE)", model, Builtin(fsys))
	}
	defer f.Close()
	return Parse(model, f)
}

// Detect picks the built-in map whose name appears in the keyboard's name
// as Keymapp reports it ("Moonlander MK1" -> moonlander).
func Detect(fsys fs.FS, keyboard string) (string, bool) {
	k := strings.ToLower(strings.ReplaceAll(keyboard, " ", ""))
	for _, n := range Builtin(fsys) {
		if strings.Contains(k, strings.ToLower(path.Base(n))) {
			return n, true
		}
	}
	return "", false
}

// Grid is the bar area: the bottom Height rows of a map.
type Grid struct {
	Map    Map
	Height int
}

// New returns the grid for a map, height rows tall from the bottom. height 0
// uses every row but the top one, leaving the number row for other lights
// (agent status, say).
func New(m Map, height int) (Grid, error) {
	if height == 0 {
		height = max(1, m.Rows()-1)
	}
	if height < 1 || height > m.Rows() {
		return Grid{}, fmt.Errorf("height %d: the %s map has %d rows", height, m.Name, m.Rows())
	}
	return Grid{Map: m, Height: height}, nil
}

// Columns is the number of bars.
func (g Grid) Columns() int { return g.Map.Columns() }

// LED is the index under bar x at level (0 = bottom row), or false for a gap.
func (g Grid) LED(x, level int) (int, bool) {
	led := g.Map.cells[g.Map.Rows()-1-level][x]
	return led, led >= 0
}
