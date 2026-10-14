// Package openrgb drives a keyboard through the OpenRGB SDK server (OpenRGB
// with "Start Server" on, by default 127.0.0.1:6742), which reaches most RGB
// keyboards: QMK with OpenRGB support, Razer, Corsair, Logitech, SteelSeries,
// ASUS and others.
//
// The SDK protocol is little-endian packets with a 16-byte header ("ORGB",
// device index, packet id, data size). The client asks for protocol version
// 1, the oldest with vendor names, so the controller description has no
// brightness or segment fields to skip.
//
// LED colours are buffered and sent once per frame (Flush) as a full
// UPDATELEDS: many OpenRGB drivers update the whole device for each LED, so
// per-LED packets would be slower, not faster.
//
// The server announces device list changes (a rescan, a device plugged in or
// out) unprompted. A Device then finds itself again by name, serial and
// location, since its index may have moved and a rescan resets its mode.
package openrgb

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jwarykowski/keybars/pkg/rgb"
)

// DefaultAddr is where OpenRGB's SDK server listens by default.
const DefaultAddr = "127.0.0.1:6742"

// Packet ids.
const (
	requestControllerCount = 0
	requestControllerData  = 1
	deviceListUpdated      = 100
	requestProtocolVersion = 40
	setClientName          = 50
	updateLEDs             = 1050
	setCustomMode          = 1100
	updateMode             = 1101
)

// protocolVersion is the SDK version asked for; see the package comment.
const protocolVersion = 1

// Device types and zone types used here.
const (
	TypeKeyboard = 5
	TypeKeypad   = 18
	zoneMatrix   = 2
	colourPerLED = 1
	flagPerLED   = 1 << 5
	noLED        = 0xFFFFFFFF
)

// timeout bounds every request, so a stalled server can't hang keybars.
const timeout = 5 * time.Second

var errNoReply = errors.New("openrgb: no reply")

// Client is a connection to an OpenRGB SDK server.
type Client struct {
	conn    net.Conn
	mu      sync.Mutex // one request or send at a time
	replies chan reply
	done    chan struct{} // closed once the connection ends; then readErr says why
	readErr error
	changed atomic.Bool // the device list changed since Controllers last read it
	version uint32
}

type reply struct {
	id   uint32
	data []byte
}

// Dial connects to the server at addr and agrees on a protocol version.
func Dial(addr string) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("openrgb %s: %w (is OpenRGB running with its SDK server on?)", addr, err)
	}
	c := &Client{conn: conn, replies: make(chan reply, 8), done: make(chan struct{})}
	go c.read()
	// Servers older than the version handshake never answer it: that is
	// protocol 0.
	data, err := c.request(0, requestProtocolVersion, u32(protocolVersion), time.Second)
	switch {
	case err == nil && len(data) >= 4:
		c.version = min(protocolVersion, binary.LittleEndian.Uint32(data))
	case errors.Is(err, errNoReply):
		c.version = 0
	case err != nil:
		conn.Close()
		return nil, fmt.Errorf("openrgb %s: %w", addr, err)
	}
	if err := c.send(0, setClientName, []byte("keybars\x00")); err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Controllers describes every device the server has.
func (c *Client) Controllers() ([]Controller, error) {
	c.changed.Store(false)
	reply, err := c.request(0, requestControllerCount, nil, timeout)
	if err != nil {
		return nil, err
	}
	if len(reply) < 4 {
		return nil, errors.New("openrgb: short controller count")
	}
	n := binary.LittleEndian.Uint32(reply)
	out := make([]Controller, 0, n)
	for i := range n {
		var data []byte
		if c.version > 0 {
			data = u32(c.version)
		}
		reply, err := c.request(i, requestControllerData, data, timeout)
		if err != nil {
			return nil, err
		}
		ctl, err := ParseController(reply, c.version)
		if err != nil {
			return nil, fmt.Errorf("openrgb device %d: %w", i, err)
		}
		ctl.Index = i
		out = append(out, ctl)
	}
	return out, nil
}

// Open picks a device: the first whose name contains name (any case), or with
// no name the first keyboard.
func (c *Client) Open(name string) (*Device, error) {
	ctls, err := c.Controllers()
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(ctls, func(ctl Controller) bool {
		if name != "" {
			return strings.Contains(strings.ToLower(ctl.Name), strings.ToLower(name))
		}
		return ctl.Type == TypeKeyboard || ctl.Type == TypeKeypad
	})
	if i < 0 {
		var names []string
		for _, ctl := range ctls {
			names = append(names, fmt.Sprintf("%q", ctl.Name))
		}
		want := "keyboard"
		if name != "" {
			want = fmt.Sprintf("device named %q", name)
		}
		return nil, fmt.Errorf("openrgb has no %s (devices: %s)", want, strings.Join(names, ", "))
	}
	ctl := ctls[i]
	if !slices.ContainsFunc(ctl.Modes, func(m Mode) bool { return m.Flags&flagPerLED != 0 }) {
		return nil, fmt.Errorf("openrgb: %s has no per-LED mode", ctl.Name)
	}
	return &Device{c: c, ctl: ctl}, nil
}

// read takes every packet the server sends: device list updates set changed,
// and replies go to the request waiting for them.
func (c *Client) read() {
	defer close(c.done)
	r := bufio.NewReader(c.conn)
	for {
		var h [16]byte
		if _, err := io.ReadFull(r, h[:]); err != nil {
			c.readErr = err
			return
		}
		if string(h[:4]) != "ORGB" {
			c.readErr = errors.New("openrgb: bad packet header")
			return
		}
		id := binary.LittleEndian.Uint32(h[8:])
		body := make([]byte, binary.LittleEndian.Uint32(h[12:]))
		if _, err := io.ReadFull(r, body); err != nil {
			c.readErr = err
			return
		}
		if id == deviceListUpdated {
			c.changed.Store(true)
			continue
		}
		select {
		case c.replies <- reply{id, body}:
		default: // nobody is waiting: a reply that came too late
		}
	}
}

// request sends a packet and returns the data of the server's reply with the
// same id, or errNoReply after wait.
func (c *Client) request(dev, id uint32, data []byte, wait time.Duration) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for drained := false; !drained; {
		select {
		case <-c.replies: // a late reply to an earlier request
		default:
			drained = true
		}
	}
	if err := c.write(dev, id, data); err != nil {
		return nil, err
	}
	expire := time.NewTimer(wait)
	defer expire.Stop()
	for {
		select {
		case r := <-c.replies:
			if r.id == id {
				return r.data, nil
			}
		case <-c.done:
			return nil, c.readErr
		case <-expire.C:
			return nil, errNoReply
		}
	}
}

// send sends a packet that has no reply.
func (c *Client) send(dev, id uint32, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.write(dev, id, data)
}

func (c *Client) write(dev, id uint32, data []byte) error {
	p := make([]byte, 16, 16+len(data))
	copy(p, "ORGB")
	binary.LittleEndian.PutUint32(p[4:], dev)
	binary.LittleEndian.PutUint32(p[8:], id)
	binary.LittleEndian.PutUint32(p[12:], uint32(len(data)))
	_, err := c.conn.Write(append(p, data...))
	return err
}

func u32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

// Device is one OpenRGB device driven as a keybars backend. It takes the
// device into its per-LED ("Direct") mode on the first Set, and RestoreAll
// puts back the mode, and per-LED colours, it had before.
type Device struct {
	c      *Client
	ctl    Controller
	direct bool
	gone   bool // unplugged or no longer listed: frames are dropped until it's back
	buf    []rgb.Colour
	dirty  bool
}

// Keyboard is the device's name, as OpenRGB shows it.
func (d *Device) Keyboard() (string, error) { return d.ctl.Name, nil }

// Controller is the device's description as it was opened.
func (d *Device) Controller() Controller { return d.ctl }

// Name is LED led's name ("Key: Q"), for keybars find.
func (d *Device) Name(led int) string {
	if led < 0 || led >= len(d.ctl.LEDs) {
		return ""
	}
	return d.ctl.LEDs[led]
}

// Close closes the connection.
func (d *Device) Close() error { return d.c.Close() }

// Set buffers one LED's colour until Flush.
func (d *Device) Set(led int, col rgb.Colour) error {
	if led < 0 || led >= len(d.ctl.LEDs) {
		return fmt.Errorf("openrgb: %s has no LED %d", d.ctl.Name, led)
	}
	if err := d.enterDirect(); err != nil {
		return err
	}
	d.buf[led] = col
	d.dirty = true
	return nil
}

// SetAll colours every LED.
func (d *Device) SetAll(col rgb.Colour) error {
	if err := d.enterDirect(); err != nil {
		return err
	}
	for i := range d.buf {
		d.buf[i] = col
	}
	d.dirty = true
	return d.Flush()
}

// Flush sends the buffered colours, if any changed.
func (d *Device) Flush() error {
	there, err := d.follow()
	if err != nil {
		return err
	}
	if !there || !d.dirty {
		return nil
	}
	d.dirty = false
	return d.c.send(d.ctl.Index, updateLEDs, encodeColours(d.buf))
}

// RestoreAll puts back the device's mode from before keybars took it, and
// its colours when that mode was per-LED.
func (d *Device) RestoreAll() error {
	if !d.direct {
		return nil
	}
	there, err := d.follow()
	d.direct, d.dirty = false, false
	if err != nil || !there {
		return err
	}
	i := d.ctl.ActiveMode
	if i < 0 || int(i) >= len(d.ctl.Modes) {
		return nil
	}
	m := d.ctl.Modes[i]
	if err := d.c.send(d.ctl.Index, updateMode, encodeMode(i, m)); err != nil {
		return err
	}
	if m.ColourMode == colourPerLED {
		return d.c.send(d.ctl.Index, updateLEDs, encodeColours(d.ctl.Colours))
	}
	return nil
}

// enterDirect switches the device to its per-LED mode. Keys outside the bars
// keep their colours when the device was already per-LED, else go dark.
func (d *Device) enterDirect() error {
	if d.direct {
		return nil
	}
	there, err := d.follow()
	if err != nil {
		return err
	}
	if there {
		if err := d.c.send(d.ctl.Index, setCustomMode, nil); err != nil {
			return err
		}
	}
	d.direct = true
	d.buf = make([]rgb.Colour, len(d.ctl.LEDs))
	if a := d.ctl.ActiveMode; a >= 0 && int(a) < len(d.ctl.Modes) && d.ctl.Modes[a].ColourMode == colourPerLED {
		copy(d.buf, d.ctl.Colours)
	}
	return nil
}

// follow finds the device again once the server's device list has changed,
// and reports whether it's there. Its index may have moved, and a rescan puts
// devices back in their saved mode, so when keybars was driving it, it goes
// back to per-LED and its whole frame is sent again on the next Flush.
func (d *Device) follow() (bool, error) {
	if !d.c.changed.Load() {
		return !d.gone, nil
	}
	ctls, err := d.c.Controllers()
	if err != nil {
		return false, err
	}
	i := same(ctls, d.ctl)
	if i < 0 {
		d.gone = true
		return false, nil
	}
	d.ctl.Index, d.gone = ctls[i].Index, false
	if d.direct {
		if err := d.c.send(d.ctl.Index, setCustomMode, nil); err != nil {
			return false, err
		}
		d.dirty = true
	}
	return true, nil
}

// same is ctl's position in ctls, matched on name, serial and location, then
// name and serial, then a name only one device has; -1 when it's not there.
// Any match must have the same number of LEDs, or the frame won't fit.
func same(ctls []Controller, ctl Controller) int {
	fits := func(c Controller) bool { return c.Name == ctl.Name && len(c.LEDs) == len(ctl.LEDs) }
	if i := slices.IndexFunc(ctls, func(c Controller) bool {
		return fits(c) && c.Serial == ctl.Serial && c.Location == ctl.Location
	}); i >= 0 {
		return i
	}
	if ctl.Serial != "" {
		if i := slices.IndexFunc(ctls, func(c Controller) bool { return fits(c) && c.Serial == ctl.Serial }); i >= 0 {
			return i
		}
	}
	found := -1
	for i, c := range ctls {
		if fits(c) {
			if found >= 0 {
				return -1
			}
			found = i
		}
	}
	return found
}

// encodeColours is UPDATELEDS' data: its own size, a count, then RGBx.
func encodeColours(cs []rgb.Colour) []byte {
	b := make([]byte, 6, 6+4*len(cs))
	binary.LittleEndian.PutUint32(b, uint32(6+4*len(cs)))
	binary.LittleEndian.PutUint16(b[4:], uint16(len(cs)))
	for _, c := range cs {
		b = append(b, c.R, c.G, c.B, 0)
	}
	return b
}

// encodeMode is UPDATEMODE's data: its own size, the mode's index, then the
// mode as the server described it.
func encodeMode(i int32, m Mode) []byte {
	b := make([]byte, 8, 8+len(m.raw))
	binary.LittleEndian.PutUint32(b, uint32(8+len(m.raw)))
	binary.LittleEndian.PutUint32(b[4:], uint32(i))
	return append(b, m.raw...)
}

// Map is the device's keys as a keybars grid, Cells[row][column] with -1 for
// a gap, from its largest matrix zone; ok is false when it has none.
func (d *Device) Map() (cells [][]int, ok bool) {
	return d.ctl.Map()
}
