package openrgb

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/jwarykowski/keybars/pkg/rgb"
)

// Controller is an OpenRGB device's description.
type Controller struct {
	Index      uint32
	Type       int32
	Name       string
	Vendor     string // protocol 1 and up
	Serial     string
	Location   string // where the server reaches it, e.g. "HID: /dev/hidraw3"
	ActiveMode int32
	Modes      []Mode
	Zones      []Zone
	LEDs       []string // LED names, in device order
	Colours    []rgb.Colour
}

// Mode is one of a device's lighting modes; raw is its description as sent,
// so UPDATEMODE can put it back exactly.
type Mode struct {
	Name       string
	Flags      uint32
	ColourMode uint32
	raw        []byte
}

// Zone is a group of a device's LEDs, starting at LED Start. Matrix, for a
// matrix zone, gives each key's LED within the zone, -1 for none.
type Zone struct {
	Name   string
	Type   int32
	Start  int
	Count  int
	Matrix [][]int
}

// ParseController reads a REQUEST_CONTROLLER_DATA reply at protocol version
// (0 or 1).
func ParseController(b []byte, version uint32) (ctl Controller, err error) {
	if version > 1 {
		return ctl, fmt.Errorf("protocol version %d not supported", version)
	}
	r := &reader{b: b}
	defer func() {
		if r.err != nil {
			err = r.err
		}
	}()
	if size := r.u32(); r.err == nil && int(size) != len(b) {
		return ctl, fmt.Errorf("controller data says %d bytes, got %d", size, len(b))
	}
	ctl.Type = r.i32()
	ctl.Name = r.str()
	if version >= 1 {
		ctl.Vendor = r.str()
	}
	r.str() // description
	r.str() // version
	ctl.Serial = r.str()
	ctl.Location = r.str()
	nModes := r.u16()
	ctl.ActiveMode = r.i32()
	for range nModes {
		start := r.off
		m := Mode{Name: r.str()}
		r.i32() // value
		m.Flags = r.u32()
		r.skip(4 * 5) // speed min, speed max, colours min, colours max, speed
		r.u32()       // direction
		m.ColourMode = r.u32()
		r.skip(4 * int(r.u16())) // mode colours
		if r.err != nil {
			return ctl, r.err
		}
		m.raw = b[start:r.off]
		ctl.Modes = append(ctl.Modes, m)
	}
	start := 0
	for range r.u16() {
		z := Zone{Name: r.str(), Type: r.i32(), Start: start}
		r.u32() // LEDs min
		r.u32() // LEDs max
		z.Count = int(r.u32())
		// The matrix length is only tested for zero, as openrgb-python does;
		// height and width say how much follows.
		if r.u16() > 0 {
			h, w := int(r.u32()), int(r.u32())
			if h*w*4 > len(b)-r.off {
				return ctl, fmt.Errorf("zone %q: %dx%d matrix runs past the data", z.Name, h, w)
			}
			z.Matrix = make([][]int, h)
			for y := range h {
				z.Matrix[y] = make([]int, w)
				for x := range w {
					v := r.u32()
					if v == noLED {
						z.Matrix[y][x] = -1
					} else {
						z.Matrix[y][x] = int(v)
					}
				}
			}
		}
		start += z.Count
		ctl.Zones = append(ctl.Zones, z)
	}
	for range r.u16() {
		ctl.LEDs = append(ctl.LEDs, r.str())
		r.u32() // value
	}
	for range r.u16() {
		c := r.bytes(4)
		if r.err == nil {
			ctl.Colours = append(ctl.Colours, rgb.Colour{R: c[0], G: c[1], B: c[2]})
		}
	}
	if r.err == nil && len(ctl.Colours) != len(ctl.LEDs) {
		return ctl, fmt.Errorf("%d colours for %d LEDs", len(ctl.Colours), len(ctl.LEDs))
	}
	return ctl, nil
}

// Map is the controller's keys as a keybars grid, Cells[row][column] with -1
// for a gap, from its largest matrix zone; ok is false when it has none.
func (ctl Controller) Map() (cells [][]int, ok bool) {
	best := -1
	for i, z := range ctl.Zones {
		if len(z.Matrix) > 0 && (best < 0 || z.Count > ctl.Zones[best].Count) {
			best = i
		}
	}
	if best < 0 {
		return nil, false
	}
	z := ctl.Zones[best]
	for _, row := range z.Matrix {
		out := make([]int, len(row))
		for x, v := range row {
			out[x] = -1
			if v >= 0 && v < z.Count {
				out[x] = z.Start + v
			}
		}
		cells = append(cells, out)
	}
	return cells, true
}

// reader decodes little-endian fields, remembering the first error.
type reader struct {
	b   []byte
	off int
	err error
}

func (r *reader) bytes(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || r.off+n > len(r.b) {
		r.err = errors.New("controller data ends early")
		return nil
	}
	v := r.b[r.off : r.off+n]
	r.off += n
	return v
}

func (r *reader) skip(n int) { r.bytes(n) }

func (r *reader) u16() uint16 {
	if b := r.bytes(2); b != nil {
		return binary.LittleEndian.Uint16(b)
	}
	return 0
}

func (r *reader) u32() uint32 {
	if b := r.bytes(4); b != nil {
		return binary.LittleEndian.Uint32(b)
	}
	return 0
}

func (r *reader) i32() int32 { return int32(r.u32()) }

// str reads a length-prefixed string, the length counting a trailing NUL.
func (r *reader) str() string {
	b := r.bytes(int(r.u16()))
	for len(b) > 0 && b[len(b)-1] == 0 {
		b = b[:len(b)-1]
	}
	return string(b)
}
