package openrgb

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jwarykowski/keybars/pkg/rgb"
)

// testdata/controller_v1.bin is a keyboard's description at protocol 1, made
// by testdata/fixture.py and checked with openrgb-python's parser. It has a
// 3x4 key matrix (10 LEDs) and a one-LED logo zone, and is in its third mode,
// "Rainbow Wave".
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseController(t *testing.T) {
	ctl, err := ParseController(fixture(t, "controller_v1.bin"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if ctl.Name != "Test Keyboard" || ctl.Vendor != "Acme" || ctl.Type != TypeKeyboard {
		t.Errorf("device = %q %q type %d", ctl.Name, ctl.Vendor, ctl.Type)
	}
	if ctl.ActiveMode != 2 || len(ctl.Modes) != 3 || ctl.Modes[0].Name != "Direct" || ctl.Modes[0].ColourMode != colourPerLED {
		t.Errorf("modes = %+v, active %d", ctl.Modes, ctl.ActiveMode)
	}
	if len(ctl.Zones) != 2 || ctl.Zones[1].Name != "Logo" || ctl.Zones[1].Start != 10 {
		t.Errorf("zones = %+v", ctl.Zones)
	}
	if len(ctl.LEDs) != 11 || ctl.LEDs[3] != "Key: Q" {
		t.Errorf("LEDs = %q", ctl.LEDs)
	}
	if ctl.Colours[10] != (rgb.Colour{R: 100, G: 200, B: 245}) {
		t.Errorf("logo colour = %v", ctl.Colours[10])
	}
	cells, ok := ctl.Map()
	want := [][]int{{0, 1, 2, -1}, {3, 4, 5, 6}, {7, -1, 8, 9}}
	if !ok || !reflect.DeepEqual(cells, want) {
		t.Errorf("map = %v, want %v", cells, want)
	}
}

func TestParseControllerTruncated(t *testing.T) {
	b := fixture(t, "controller_v1.bin")
	for _, n := range []int{0, 3, 20, len(b) / 2, len(b) - 1} {
		cut := append([]byte(nil), b[:n]...)
		if n >= 4 {
			binary.LittleEndian.PutUint32(cut, uint32(n))
		}
		if _, err := ParseController(cut, 1); err == nil {
			t.Errorf("%d of %d bytes: no error", n, len(b))
		}
	}
}

// UPDATEMODE carries the mode as described, after its size and index: what
// openrgb-python sends for mode 2 is testdata/mode_rainbow_v1.bin.
func TestEncodeMode(t *testing.T) {
	ctl, err := ParseController(fixture(t, "controller_v1.bin"), 1)
	if err != nil {
		t.Fatal(err)
	}
	raw := fixture(t, "mode_rainbow_v1.bin")
	got := encodeMode(2, ctl.Modes[2])
	want := append(binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, uint32(8+len(raw))), 2), raw...)
	if !bytes.Equal(got, want) {
		t.Errorf("UPDATEMODE data\n got %x\nwant %x", got, want)
	}
}

type packet struct {
	dev, id uint32
	data    []byte
}

// fakeServer is an OpenRGB SDK server holding the device descriptions in
// descs. It answers the version handshake (unless silent), the count and the
// descriptions, and records every other packet.
type fakeServer struct {
	mu    sync.Mutex
	descs [][]byte
	conn  net.Conn
}

func server(t *testing.T, desc []byte, silent bool) (string, <-chan packet) {
	t.Helper()
	_, addr, got := fake(t, silent, desc)
	return addr, got
}

func fake(t *testing.T, silent bool, descs ...[]byte) (*fakeServer, string, <-chan packet) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f := &fakeServer{descs: descs}
	got := make(chan packet, 100)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		f.mu.Lock()
		f.conn = conn
		f.mu.Unlock()
		reply := func(id uint32, data []byte) {
			f.mu.Lock()
			defer f.mu.Unlock()
			_, _ = conn.Write(append(header(0, id, len(data)), data...)) // the client under test reports failures
		}
		for {
			var h [16]byte
			if _, err := io.ReadFull(conn, h[:]); err != nil {
				return
			}
			p := packet{dev: binary.LittleEndian.Uint32(h[4:]), id: binary.LittleEndian.Uint32(h[8:])}
			p.data = make([]byte, binary.LittleEndian.Uint32(h[12:]))
			if _, err := io.ReadFull(conn, p.data); err != nil {
				return
			}
			f.mu.Lock()
			descs := f.descs
			f.mu.Unlock()
			switch p.id {
			case requestProtocolVersion:
				if !silent {
					reply(p.id, u32(4))
				}
			case requestControllerCount:
				reply(p.id, u32(uint32(len(descs))))
			case requestControllerData:
				reply(p.id, descs[p.dev])
			default:
				got <- p
			}
		}
	}()
	return f, ln.Addr().String(), got
}

// change replaces the device list and announces it, as OpenRGB does after a
// rescan.
func (f *fakeServer) change(descs ...[]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.descs = descs
	_, _ = f.conn.Write(header(0, deviceListUpdated, 0)) // the client under test reports failures
}

func header(dev, id uint32, n int) []byte {
	h := []byte("ORGB")
	h = binary.LittleEndian.AppendUint32(h, dev)
	h = binary.LittleEndian.AppendUint32(h, id)
	return binary.LittleEndian.AppendUint32(h, uint32(n))
}

func next(t *testing.T, got <-chan packet) packet {
	t.Helper()
	select {
	case p := <-got:
		return p
	case <-time.After(2 * time.Second):
		t.Fatal("no packet")
		return packet{}
	}
}

func TestSession(t *testing.T) {
	addr, got := server(t, fixture(t, "controller_v1.bin"), false)
	c, err := Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.version != 1 {
		t.Errorf("agreed version %d, want 1 (min of ours and the server's 4)", c.version)
	}
	if p := next(t, got); p.id != setClientName || string(p.data) != "keybars\x00" {
		t.Errorf("client name packet = %+v", p)
	}
	d, err := c.Open("")
	if err != nil {
		t.Fatal(err)
	}
	if name := d.Name(8); name != "Key: Space" {
		t.Errorf("LED 8 = %q", name)
	}

	// The first Set takes the device into its per-LED mode; colours go out
	// together on Flush, every LED at once, dark except what was set (the
	// device was in a hardware effect).
	red := rgb.Colour{R: 255}
	if err := d.Set(4, red); err != nil {
		t.Fatal(err)
	}
	if err := d.Set(9, red); err != nil {
		t.Fatal(err)
	}
	if p := next(t, got); p.id != setCustomMode || len(p.data) != 0 {
		t.Errorf("first packet = %+v, want SETCUSTOMMODE", p)
	}
	if err := d.Flush(); err != nil {
		t.Fatal(err)
	}
	want := make([]rgb.Colour, 11)
	want[4], want[9] = red, red
	if p := next(t, got); p.id != updateLEDs || !bytes.Equal(p.data, encodeColours(want)) {
		t.Errorf("flush = %+v, want UPDATELEDS %x", p, encodeColours(want))
	}
	if err := d.Flush(); err != nil { // nothing new: nothing sent
		t.Fatal(err)
	}
	if err := d.Set(11, red); err == nil {
		t.Error("Set past the last LED: no error")
	}

	// RestoreAll puts the effect back; it isn't per-LED, so no colours.
	if err := d.RestoreAll(); err != nil {
		t.Fatal(err)
	}
	if p := next(t, got); p.id != updateMode || !bytes.Equal(p.data, encodeMode(2, d.ctl.Modes[2])) {
		t.Errorf("restore = %+v, want UPDATEMODE of mode 2", p)
	}
	// Back in direct mode on the next Set.
	if err := d.Set(0, red); err != nil {
		t.Fatal(err)
	}
	if p := next(t, got); p.id != setCustomMode {
		t.Errorf("after restore = %+v, want SETCUSTOMMODE", p)
	}
	select {
	case p := <-got:
		t.Errorf("unexpected packet %+v", p)
	case <-time.After(50 * time.Millisecond):
	}
}

// A device already in its per-LED mode keeps its colours on keys outside the
// bars, and gets them back on restore.
func TestSessionFromDirect(t *testing.T) {
	desc := fixture(t, "controller_v1.bin")
	ctl, err := ParseController(desc, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Point the active mode at "Direct": it follows the name, vendor and four
	// metadata strings, and the mode count.
	desc = append([]byte(nil), desc...)
	off := activeModeOffset(t, desc)
	binary.LittleEndian.PutUint32(desc[off:], 0)

	addr, got := server(t, desc, false)
	c, err := Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	next(t, got) // client name
	d, err := c.Open("test")
	if err != nil {
		t.Fatal(err)
	}
	blue := rgb.Colour{B: 255}
	if err := d.Set(1, blue); err != nil {
		t.Fatal(err)
	}
	d.Flush()
	next(t, got) // SETCUSTOMMODE
	want := append([]rgb.Colour(nil), ctl.Colours...)
	want[1] = blue
	if p := next(t, got); !bytes.Equal(p.data, encodeColours(want)) {
		t.Errorf("flush kept no colours: %x", p.data)
	}
	if err := d.RestoreAll(); err != nil {
		t.Fatal(err)
	}
	if p := next(t, got); p.id != updateMode || !bytes.Equal(p.data, encodeMode(0, ctl.Modes[0])) {
		t.Errorf("restore mode = %+v", p)
	}
	if p := next(t, got); p.id != updateLEDs || !bytes.Equal(p.data, encodeColours(ctl.Colours)) {
		t.Errorf("restore colours = %+v", p)
	}
}

func activeModeOffset(t *testing.T, b []byte) int {
	t.Helper()
	off := 8 // size, type
	for range 6 {
		off += 2 + int(binary.LittleEndian.Uint16(b[off:]))
	}
	return off + 2 // mode count
}

// Servers from before the version handshake never answer it.
func TestOldServer(t *testing.T) {
	addr, _ := server(t, fixture(t, "controller_v1.bin"), true)
	c, err := Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.version != 0 {
		t.Errorf("version = %d, want 0", c.version)
	}
}

func TestOpenErrors(t *testing.T) {
	addr, _ := server(t, fixture(t, "controller_v1.bin"), false)
	c, err := Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Open("mouse"); err == nil {
		t.Error("no device named mouse: no error")
	}
}

// renamed is the fixture's description with another name of the same length.
func renamed(t *testing.T, name string) []byte {
	t.Helper()
	b := bytes.Replace(fixture(t, "controller_v1.bin"), []byte("Test Keyboard\x00"), []byte(name+"\x00"), 1)
	if len(name) != len("Test Keyboard") || bytes.Equal(b, fixture(t, "controller_v1.bin")) {
		t.Fatalf("rename to %q failed", name)
	}
	return b
}

// waitChanged waits for the client to take the server's device list update.
func waitChanged(t *testing.T, c *Client) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); !c.changed.Load(); {
		if time.Now().After(deadline) {
			t.Fatal("device list update never arrived")
		}
		time.Sleep(time.Millisecond)
	}
}

func quiet(t *testing.T, got <-chan packet) {
	t.Helper()
	select {
	case p := <-got:
		t.Errorf("unexpected packet %+v", p)
	case <-time.After(50 * time.Millisecond):
	}
}

// When OpenRGB's device list changes, the device is found again at its new
// index, put back in its per-LED mode and sent its whole frame. While it's
// gone, frames are dropped; when it's back, painting resumes.
func TestFollowsDeviceListChanges(t *testing.T) {
	kb, other := fixture(t, "controller_v1.bin"), renamed(t, "Other Keyboar")
	f, addr, got := fake(t, false, kb)
	c, err := Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	next(t, got) // client name
	d, err := c.Open("test")
	if err != nil {
		t.Fatal(err)
	}
	red := rgb.Colour{R: 255}
	frame := make([]rgb.Colour, 11)
	if err := d.Set(1, red); err != nil {
		t.Fatal(err)
	}
	d.Flush()
	frame[1] = red
	if p := next(t, got); p.id != setCustomMode || p.dev != 0 {
		t.Fatalf("enter = %+v", p)
	}
	next(t, got) // UPDATELEDS

	// moved to index 1
	f.change(other, kb)
	waitChanged(t, c)
	if err := d.Set(2, red); err != nil {
		t.Fatal(err)
	}
	frame[2] = red
	if err := d.Flush(); err != nil {
		t.Fatal(err)
	}
	if p := next(t, got); p.id != setCustomMode || p.dev != 1 {
		t.Errorf("after the move = %+v, want SETCUSTOMMODE to device 1", p)
	}
	if p := next(t, got); p.id != updateLEDs || p.dev != 1 || !bytes.Equal(p.data, encodeColours(frame)) {
		t.Errorf("after the move = %+v, want the whole frame to device 1", p)
	}

	// unplugged: nothing is sent, nothing fails
	f.change(other)
	waitChanged(t, c)
	if err := d.Set(3, red); err != nil {
		t.Fatal(err)
	}
	frame[3] = red
	if err := d.Flush(); err != nil {
		t.Fatal(err)
	}
	quiet(t, got)

	// back at index 0: per-LED again, with everything set meanwhile
	f.change(kb)
	waitChanged(t, c)
	if err := d.Flush(); err != nil {
		t.Fatal(err)
	}
	if p := next(t, got); p.id != setCustomMode || p.dev != 0 {
		t.Errorf("on return = %+v, want SETCUSTOMMODE to device 0", p)
	}
	if p := next(t, got); p.id != updateLEDs || p.dev != 0 || !bytes.Equal(p.data, encodeColours(frame)) {
		t.Errorf("on return = %+v, want the whole frame to device 0", p)
	}
	if err := d.RestoreAll(); err != nil {
		t.Fatal(err)
	}
	if p := next(t, got); p.id != updateMode || p.dev != 0 {
		t.Errorf("restore = %+v, want UPDATEMODE to device 0", p)
	}
	quiet(t, got)
}

func TestSame(t *testing.T) {
	ctl := Controller{Name: "K", Serial: "S1", Location: "HID: 1", LEDs: make([]string, 3)}
	for name, tc := range map[string]struct {
		ctls []Controller
		want int
	}{
		"exact":             {[]Controller{{Name: "M"}, ctl}, 1},
		"replugged":         {[]Controller{{Name: "K", Serial: "S1", Location: "HID: 7", LEDs: make([]string, 3)}}, 0},
		"twin by serial":    {[]Controller{{Name: "K", Serial: "S2", Location: "HID: 1", LEDs: make([]string, 3)}, {Name: "K", Serial: "S1", Location: "HID: 9", LEDs: make([]string, 3)}}, 1},
		"two unknown twins": {[]Controller{{Name: "K", Serial: "S8", LEDs: make([]string, 3)}, {Name: "K", Serial: "S9", LEDs: make([]string, 3)}}, -1},
		"other LED count":   {[]Controller{{Name: "K", Serial: "S1", Location: "HID: 1", LEDs: make([]string, 4)}}, -1},
		"gone":              {[]Controller{{Name: "M"}}, -1},
	} {
		if got := same(tc.ctls, ctl); got != tc.want {
			t.Errorf("%s: same = %d, want %d", name, got, tc.want)
		}
	}
}

// Replies that arrive too late for their request don't answer the next one.
func TestLateReplyIsDropped(t *testing.T) {
	c := &Client{replies: make(chan reply, 8)}
	c.replies <- reply{id: requestControllerCount, data: u32(9)}
	client, srv := net.Pipe()
	defer client.Close()
	c.conn, c.done = client, make(chan struct{})
	go c.read()
	go func() {
		var h [16]byte
		if _, err := io.ReadFull(srv, h[:]); err != nil {
			return
		}
		_, _ = srv.Write(append(header(0, requestControllerCount, 4), u32(1)...))
	}()
	data, err := c.request(0, requestControllerCount, nil, time.Second)
	if err != nil || binary.LittleEndian.Uint32(data) != 1 {
		t.Fatalf("got %v, %v; want the fresh reply, 1", data, err)
	}
}
