// keybars: a music equaliser on a keyboard's backlight. cava listens to
// what the system plays; each column of keys becomes a bar rising from the
// bottom row. Keys go back to the layout's colours when the music stops and
// when keybars exits.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	builtin "github.com/jwarykowski/keybars/maps"
	"github.com/jwarykowski/keybars/pkg/bars"
	"github.com/jwarykowski/keybars/pkg/keymapp"
	"github.com/jwarykowski/keybars/pkg/layout"
	"github.com/jwarykowski/keybars/pkg/openrgb"
	"github.com/jwarykowski/keybars/pkg/qmk"
	"github.com/jwarykowski/keybars/pkg/rgb"
	"github.com/jwarykowski/keybars/pkg/source"
)

// maps holds the built-in keyboard maps, <model>.txt.
var maps fs.FS = builtin.FS

const usage = `keybars: a music equaliser on your keyboard's backlight

usage:
  keybars [flags]         show the music playing as bars (needs cava)
  keybars test [flags]    sweep the bars across the keyboard to check a map
  keybars find [flags]    light LEDs one at a time to write a map
  keybars gen KEYBOARD    write a map from QMK's definition of a keyboard
  keybars maps            list the built-in maps and the connected keyboard
  keybars start [flags]   run in the background
  keybars stop            stop the background keybars
  keybars status          show whether keybars is running in the background
  keybars version         print the version
  keybars licences        print the licences of the code keybars includes

Run a command with -h for its flags. keybars and keybars start read their
settings from ~/.config/keybars/config, one flag per line ("gain = 2");
flags on the command line override it.
`

func main() { os.Exit(cli(os.Args[1:])) }

// cli runs the command in args and returns keybars' exit status.
func cli(args []string) int {
	cmd := "run"
	if len(args) > 0 && (!strings.HasPrefix(args[0], "-") || args[0] == "-version" || args[0] == "--version") {
		cmd, args = strings.TrimLeft(args[0], "-"), args[1:]
	}
	var err error
	switch cmd {
	case "run":
		err = run(args)
	case "test":
		err = test(args)
	case "find":
		err = find(args)
	case "gen":
		err = gen(args)
	case "maps":
		err = listMaps(args)
	case "start":
		err = start(args)
	case "stop":
		err = stop()
	case "status":
		err = status()
	case "version":
		fmt.Println("keybars", version())
	case "licences", "licenses":
		fmt.Print(notices)
	case "help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, "keybars:", err)
		return 1
	}
	return 0
}

// commands are main's subcommands, for noArgs to spot one after the flags.
var commands = []string{"run", "test", "find", "gen", "maps", "start", "stop", "status", "version", "licences", "licenses", "help"}

// noArgs rejects words left after fl's flags: a command after a flag
// (keybars -v test) would otherwise be dropped and the bars run.
func noArgs(fl *flag.FlagSet) error {
	if fl.NArg() == 0 {
		return nil
	}
	a := fl.Arg(0)
	if slices.Contains(commands, a) {
		return fmt.Errorf("%q after the flags: put the command first (keybars %s [flags])", a, a)
	}
	return fmt.Errorf("unexpected argument %q", a)
}

// keyboard is a backend: Keymapp, or a device through OpenRGB.
type keyboard interface {
	rgb.LEDs
	// Keyboard is the keyboard's name, as the backend reports it.
	Keyboard() (string, error)
	SetAll(c rgb.Colour) error
	Close() error
}

// conn are the flags that pick and reach a backend.
type conn struct {
	backend, socket, addr, device *string
}

func connFlags(fl *flag.FlagSet) conn {
	return conn{
		backend: fl.String("backend", "keymapp", "keymapp (ZSA boards) or openrgb (OpenRGB's SDK server)"),
		socket:  fl.String("socket", "", "Keymapp socket (default: $KEYMAPP_SOCKET, else Keymapp's own)"),
		addr:    fl.String("openrgb", openrgb.DefaultAddr, "OpenRGB SDK server address"),
		device:  fl.String("device", "", "OpenRGB device: part of its name (default: the first keyboard)"),
	}
}

// check rejects an unknown backend before connecting.
func (c conn) check() error {
	if *c.backend != "keymapp" && *c.backend != "openrgb" {
		return fmt.Errorf("-backend %q: want keymapp or openrgb", *c.backend)
	}
	return nil
}

func (c conn) connect() (keyboard, error) {
	switch *c.backend {
	case "keymapp":
		path := *c.socket
		if path == "" {
			var err error
			if path, err = keymapp.SocketPath(); err != nil {
				return nil, err
			}
		}
		return keymapp.Dial(path)
	case "openrgb":
		client, err := openrgb.Dial(*c.addr)
		if err != nil {
			return nil, err
		}
		d, err := client.Open(*c.device)
		if err != nil {
			client.Close()
			return nil, err
		}
		return d, nil
	}
	return nil, fmt.Errorf("-backend %q: want keymapp or openrgb", *c.backend)
}

// common are the flags every command that paints a map takes.
type common struct {
	model, mapFile *string
	conn
}

func commonFlags(fl *flag.FlagSet) common {
	return common{
		model:   fl.String("model", "", "built-in Keymapp map: "+strings.Join(layout.Builtin(maps), ", ")+" (default: detect the connected keyboard)"),
		mapFile: fl.String("map", "", "map file to use instead of a built-in one"),
		conn:    connFlags(fl),
	}
}

// check rejects a backend, model or map file that can't work before
// connecting, so a bad flag isn't hidden behind a missing keyboard.
func (c common) check() error {
	if err := c.conn.check(); err != nil {
		return err
	}
	if *c.model != "" && !slices.Contains(layout.Builtin(maps), *c.model) {
		return fmt.Errorf("-model %q: want one of %s, or -map FILE", *c.model, strings.Join(layout.Builtin(maps), ", "))
	}
	if *c.mapFile != "" {
		if _, err := readMap(*c.mapFile); err != nil {
			return err
		}
	}
	return nil
}

func readMap(path string) (layout.Map, error) {
	f, err := os.Open(path)
	if err != nil {
		return layout.Map{}, err
	}
	defer f.Close()
	return layout.Parse(path, f)
}

// loadMap reads -map, else the -model map, else the map for the connected
// keyboard: the built-in one matching Keymapp's name for it, or an OpenRGB
// device's own key matrix.
func loadMap(c common, kb keyboard) (layout.Map, error) {
	if *c.mapFile != "" {
		m, err := readMap(*c.mapFile)
		if err == nil {
			// OpenRGB's matrices can give a wide key's LED twice, so it's only a warning
			if led, ok := m.Duplicate(); ok {
				fmt.Fprintf(os.Stderr, "keybars: %s: LED %d is in the map twice, so its key shows two bars\n", *c.mapFile, led)
			}
		}
		return m, err
	}
	if d, ok := kb.(*openrgb.Device); ok {
		if *c.model != "" {
			return layout.Map{}, errors.New("-model maps are Keymapp's numbering; OpenRGB devices use their own key matrix, or -map FILE")
		}
		return openrgbMap(d)
	}
	model := *c.model
	if model == "" {
		name, err := kb.Keyboard()
		if err != nil {
			return layout.Map{}, err
		}
		var ok bool
		if model, ok = layout.Detect(maps, name); !ok {
			return layout.Map{}, fmt.Errorf("no built-in map for %q: write one with `keybars find` and pass -map FILE", name)
		}
	}
	return layout.Load(maps, model)
}

// openrgbMap is the map for an OpenRGB device. A QMK board keybars has a
// map for gets that map, in the firmware's LED order as OpenRGB numbers it,
// when its LED count matches: OpenRGB's own matrix for a QMK board follows
// its wiring (VialRGB) or rows of LED positions (QMK-OpenRGB), which can
// lean the bars or stack a split board's halves. Other devices use OpenRGB's
// key matrix.
func openrgbMap(d *openrgb.Device) (layout.Map, error) {
	name, _ := d.Keyboard()
	if b, ok := builtin.MatchQMK(name); ok {
		if n := len(d.Controller().LEDs); n == b.LEDs {
			fmt.Fprintf(os.Stderr, "keybars: using the map from QMK for %s (keybars test checks it)\n", b.Name)
			return layout.Load(builtin.QMK, b.ID)
		} else if _, matrix := d.Map(); !matrix {
			return layout.Map{}, fmt.Errorf("%s has %d LEDs, QMK's %s has %d: write a map with `keybars find -backend openrgb` and pass -map FILE", name, n, b.Name, b.LEDs)
		}
	}
	cells, ok := d.Map()
	if !ok {
		return layout.Map{}, fmt.Errorf("OpenRGB gives no key matrix for %s: write a map with `keybars find -backend openrgb` and pass -map FILE", name)
	}
	return layout.FromCells(name, cells)
}

func run(args []string) error {
	fl := flag.NewFlagSet("keybars", flag.ContinueOnError)
	c := commonFlags(fl)
	height := fl.Int("height", 0, "bar height in rows from the bottom; 0 = all but the top row")
	colours := fl.String("colours", "#00ff00", "bar colours, bottom to top (e.g. #00ff00,#ffff00,#ff0000 for a meter)")
	gain := fl.Float64("gain", 1.5, "amplify the music before it becomes bars: higher reaches the top more often")
	reserve := fl.String("reserve", "", "comma-separated LEDs never to touch")
	fps := fl.Int("fps", 30, "frames per second asked of cava")
	smooth := fl.Int("smooth", 35, "cava smoothing, 0-100: lower is jumpier, higher is smoother")
	cavaSource := fl.String("source", "", "cava capture device (e.g. \"BlackHole 2ch\" on macOS); empty = cava's default")
	idle := fl.Duration("idle", time.Second, "silence before the keys go back to the layout")
	refresh := fl.Duration("refresh", 2*time.Second, "resend every key this often, in case something reset the board")
	stdin := fl.Bool("stdin", false, "read frames (cava raw ascii, \"3;5;1;...\") from stdin instead of running cava")
	peaks := fl.Bool("peaks", true, "falling peak dots above the bars (-peaks=false to hide)")
	peakColour := fl.String("peak-colour", "#ffffff", "peak dot colour")
	peakHold := fl.Duration("peak-hold", 400*time.Millisecond, "how long a peak dot holds before falling")
	peakFall := fl.Float64("peak-fall", 6, "how fast peak dots fall, in rows per second")
	verbose := fl.Bool("v", false, "print frames painted and LEDs sent on exit")
	fl.String("config", "", "settings file (default: ~/.config/keybars/config)")
	if err := loadConfig(fl, args); err != nil {
		return err
	}
	if err := fl.Parse(args); err != nil {
		return err
	}

	if err := noArgs(fl); err != nil {
		return err
	}
	if *stdin && os.Getenv(backgroundEnv) != "" {
		return errors.New("-stdin can't run in the background: start has no input to give it")
	}
	if err := checkRun(*fps, *smooth, *gain); err != nil {
		return err
	}
	if err := checkTimes(*height, *idle, *refresh, *peakHold, *peakFall); err != nil {
		return err
	}
	cs, err := bars.ParseColours(*colours)
	if err != nil {
		return err
	}
	pc, err := bars.ParseColours(*peakColour)
	if err != nil {
		return fmt.Errorf("-peak-colour: %w", err)
	}
	if len(pc) != 1 {
		return fmt.Errorf("-peak-colour %q: want one colour", *peakColour)
	}
	var reserved []int
	for s := range strings.SplitSeq(*reserve, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return fmt.Errorf("-reserve %q: want LED indices, e.g. 36,61", *reserve)
		}
		reserved = append(reserved, n)
	}
	if err := c.check(); err != nil {
		return err
	}

	client, err := c.connect()
	if err != nil {
		return err
	}
	defer client.Close()
	m, err := loadMap(c, client)
	if err != nil {
		return err
	}
	grid, err := layout.New(m, *height)
	if err != nil {
		return err
	}
	r := bars.New(grid, client, cs, reserved)
	var pk *bars.Peaks
	if *peaks {
		r.WithPeaks(pc[0])
		pk = bars.NewPeaks(*peakHold, *peakFall)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// cava reports in finer steps than keys (fine per key), so the gain can
	// lift quiet bars without losing their shape; -stdin frames are in keys
	const fine = 8
	frames := make(chan []int)
	raw := make(chan []int)
	srcErr := make(chan error, 1)
	go func() {
		if *stdin {
			srcErr <- source.Read(os.Stdin, frames)
		} else {
			srcErr <- source.Cava(ctx, grid.Columns(), grid.Height*fine, *fps, *smooth, *cavaSource, raw)
		}
	}()
	if !*stdin {
		go func() {
			defer close(frames)
			for f := range raw {
				frames <- bars.Scale(f, fine, *gain, grid.Height)
			}
		}()
	}

	var st stats
	start := time.Now()
	err = paint(ctx, r, pk, latest(ctx, frames), *idle, *refresh, &st)
	if *verbose {
		secs := time.Since(start).Seconds()
		fmt.Fprintf(os.Stderr, "keybars: %d frames in %.1fs (%.1f fps), %d LEDs sent (%.1f per frame)\n",
			st.frames, secs, float64(st.frames)/secs, st.leds, float64(st.leds)/max(1, float64(st.frames)))
	}
	if cerr := r.Clear(); err == nil {
		err = cerr
	}
	// stop cava and wait for it, so its error (a crash, say) isn't lost;
	// a read from stdin can't be stopped, so only take its error if it's there
	stop()
	if !*stdin {
		if serr := <-srcErr; err == nil {
			err = serr
		}
	} else if err == nil {
		select {
		case err = <-srcErr:
		default:
		}
	}
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return err
}

// checkRun rejects settings cava or the bars can't use.
func checkRun(fps, smooth int, gain float64) error {
	switch {
	case fps < 1:
		return fmt.Errorf("-fps %d: want 1 or more", fps)
	case smooth < 0 || smooth > 100:
		return fmt.Errorf("-smooth %d: want 0-100", smooth)
	case !(gain > 0) || math.IsInf(gain, 0):
		return fmt.Errorf("-gain %g: want more than 0", gain)
	}
	return nil
}

// checkTimes rejects a height or timing the bars can't use. -refresh 0
// never resends.
func checkTimes(height int, idle, refresh, peakHold time.Duration, peakFall float64) error {
	switch {
	case height < 0:
		return fmt.Errorf("-height %d: want 0 or more", height)
	case idle < 0:
		return fmt.Errorf("-idle %s: want 0 or more", idle)
	case refresh < 0:
		return fmt.Errorf("-refresh %s: want 0 (never) or more", refresh)
	case peakHold < 0:
		return fmt.Errorf("-peak-hold %s: want 0 or more", peakHold)
	case !(peakFall > 0) || math.IsInf(peakFall, 0):
		return fmt.Errorf("-peak-fall %g: want more than 0", peakFall)
	}
	return nil
}

// stats counts what paint did, for -v.
type stats struct{ frames, leds int }

// paint draws the newest frame whenever one arrives, until the source ends or
// ctx is done, with peak dots when pk isn't nil. After idle of silence it
// hands the keys back to the layout; every refresh it resends every key.
func paint(ctx context.Context, r *bars.Renderer, pk *bars.Peaks, next func() ([]int, bool), idle, refresh time.Duration, st *stats) error {
	var quietSince time.Time
	lastFull := time.Now()
	cleared := true
	for ctx.Err() == nil {
		f, ok := next()
		if !ok {
			return nil
		}
		if !slices.ContainsFunc(f, func(h int) bool { return h > 0 }) {
			if quietSince.IsZero() {
				quietSince = time.Now()
			}
			if cleared || time.Since(quietSince) >= idle {
				if !cleared {
					if err := r.Clear(); err != nil {
						return err
					}
					cleared = true
				}
				continue
			}
		} else {
			quietSince = time.Time{}
		}
		cleared = false
		if refresh > 0 && time.Since(lastFull) >= refresh {
			r.Invalidate()
			lastFull = time.Now()
		}
		var peaks []int
		if pk != nil {
			peaks = pk.Update(f, time.Now())
		}
		n, err := r.Frame(f, peaks)
		if err != nil {
			return err
		}
		st.frames++
		st.leds += n
	}
	return nil
}

// latest drains frames in the background and returns a function that blocks
// for the newest one, so a slow keyboard drops frames instead of lagging
// behind the music. It reports false once frames is closed or ctx is done.
func latest(ctx context.Context, frames <-chan []int) func() ([]int, bool) {
	var (
		mu     sync.Mutex
		cur    []int
		fresh  bool
		closed bool
		wake   = make(chan struct{}, 1)
	)
	go func() {
		for f := range frames {
			mu.Lock()
			cur, fresh = f, true
			mu.Unlock()
			select {
			case wake <- struct{}{}:
			default:
			}
		}
		mu.Lock()
		closed = true
		mu.Unlock()
		select {
		case wake <- struct{}{}:
		default:
		}
	}()
	return func() ([]int, bool) {
		for {
			mu.Lock()
			if fresh {
				fresh = false
				f := cur
				mu.Unlock()
				return f, true
			}
			if closed {
				mu.Unlock()
				return nil, false
			}
			mu.Unlock()
			select {
			case <-wake:
			case <-ctx.Done():
				return nil, false
			}
		}
	}
}

// test sweeps a full bar across each column, then a level across every
// column, bottom up, so a wrong map shows as a key out of line.
func test(args []string) error {
	fl := flag.NewFlagSet("keybars test", flag.ContinueOnError)
	c := commonFlags(fl)
	step := fl.Duration("step", 300*time.Millisecond, "time per step")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if err := noArgs(fl); err != nil {
		return err
	}
	if err := c.check(); err != nil {
		return err
	}
	if err := notInBackground(); err != nil {
		return err
	}
	client, err := c.connect()
	if err != nil {
		return err
	}
	defer client.Close()
	m, err := loadMap(c, client)
	if err != nil {
		return err
	}
	grid, err := layout.New(m, m.Rows())
	if err != nil {
		return err
	}
	cs, _ := bars.ParseColours("#00ff00,#ffff00,#ff0000")
	r := bars.New(grid, client, cs, nil)
	defer func() { _ = r.Clear() }() // best effort on the way out

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var failed error
	show := func(heights []int) bool {
		if _, failed = r.Frame(heights, nil); failed != nil {
			return false
		}
		select {
		case <-time.After(*step):
			return true
		case <-ctx.Done():
			return false
		}
	}
	fmt.Printf("%s: %d columns x %d rows; columns left to right, then rows bottom up\n", m.Name, m.Columns(), m.Rows())
	for x := range grid.Columns() {
		h := make([]int, grid.Columns())
		h[x] = grid.Height
		if !show(h) {
			return failed
		}
	}
	for level := 1; level <= grid.Height; level++ {
		h := make([]int, grid.Columns())
		for x := range h {
			h[x] = level
		}
		if !show(h) {
			return failed
		}
	}
	return nil
}

func listMaps(args []string) error {
	fl := flag.NewFlagSet("keybars maps", flag.ContinueOnError)
	cf := connFlags(fl)
	save := fl.String("save", "", "with -backend openrgb, also save each device's description in this directory, to attach to a report")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if err := noArgs(fl); err != nil {
		return err
	}
	if err := cf.check(); err != nil {
		return err
	}
	if *cf.backend == "openrgb" {
		return listOpenRGB(cf, *save)
	}
	if *save != "" {
		return errors.New("-save is for -backend openrgb")
	}
	for _, n := range layout.Builtin(maps) {
		m, err := layout.Load(maps, n)
		if err != nil {
			return err
		}
		fmt.Printf("%-12s %d columns x %d rows\n", n, m.Columns(), m.Rows())
	}
	client, err := cf.connect()
	if err != nil {
		return nil // listing works without Keymapp
	}
	defer client.Close()
	kb, err := client.Keyboard()
	if err != nil {
		return nil
	}
	if n, ok := layout.Detect(maps, kb); ok {
		fmt.Printf("\nconnected: %s (uses %s)\n", kb, n)
	} else {
		fmt.Printf("\nconnected: %s (no built-in map; see keybars find)\n", kb)
	}
	return nil
}

// listOpenRGB lists OpenRGB's devices, saving their descriptions in save
// when it's set, then prints the map keybars would use for the chosen one,
// ready to save and edit for -map.
func listOpenRGB(cf conn, save string) error {
	client, err := openrgb.Dial(*cf.addr)
	if err != nil {
		return err
	}
	defer client.Close()
	ctls, err := client.Controllers()
	if err != nil {
		return err
	}
	for _, ctl := range ctls {
		matrix := "no key matrix"
		if cells, ok := ctl.Map(); ok {
			matrix = fmt.Sprintf("%dx%d key matrix", len(cells[0]), len(cells))
		}
		fmt.Printf("%-3d %s: %d LEDs, %s\n", ctl.Index, ctl.Name, len(ctl.LEDs), matrix)
	}
	if save != "" {
		if err := saveDescriptions(save, ctls, client.Version()); err != nil {
			return err
		}
		fmt.Printf("\nsaved %d descriptions in %s (serial numbers masked)\n", len(ctls), save)
	}
	d, err := client.Open(*cf.device)
	if err != nil {
		return err
	}
	name, _ := d.Keyboard()
	cells, ok := d.Map()
	if !ok {
		fmt.Printf("\n%s gives no key matrix: write a map with keybars find -backend openrgb\n", name)
		return nil
	}
	fmt.Println()
	return qmk.Grid{Cells: cells}.Format(os.Stdout,
		name+": OpenRGB's LED index for each key, rows top to bottom.",
		"From its key matrix; save and edit it to use with -map.")
}

// saveDescriptions writes each device's description to dir as
// <index>-<name>.v<protocol>.bin, the form the openrgb package's tests read.
func saveDescriptions(dir string, ctls []openrgb.Controller, version uint32) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, ctl := range ctls {
		name := fmt.Sprintf("%d-%s.v%d.bin", ctl.Index, slug(ctl.Name), version)
		if err := os.WriteFile(filepath.Join(dir, name), ctl.Raw(), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// slug is s in lower case with each run of other characters as one dash.
func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// gen writes a map from a QMK keyboard.json: a file, or a keyboard's path in
// qmk_firmware (zsa/voyager) to fetch.
func gen(args []string) error {
	fl := flag.NewFlagSet("keybars gen", flag.ContinueOnError)
	order := fl.String("order", "keymapp", "key numbering: keymapp (its API index) or firmware (QMK's rgb_matrix index)")
	name := fl.String("layout", "", "QMK layout (default: LAYOUT, else the first)")
	inner := fl.Bool("inner", false, "keep a split half's short inner column")
	shape := fl.Bool("json", false, "print the board's shape as JSON (each key's place and size, and where the map puts it), not the map")
	fl.Usage = func() {
		fmt.Fprintln(fl.Output(), "usage: keybars gen [flags] KEYBOARD\n\nKEYBOARD is a QMK keyboard.json, or a keyboard's path in qmk_firmware/keyboards (zsa/voyager) to fetch.")
		fl.PrintDefaults()
	}
	if err := fl.Parse(args); err != nil {
		return err
	}
	if fl.NArg() != 1 {
		fl.Usage()
		return flag.ErrHelp
	}
	o, err := qmk.ParseOrder(*order)
	if err != nil {
		return err
	}
	src := fl.Arg(0)
	kb, err := qmk.Read(src)
	if err != nil {
		return err
	}
	opts := qmk.Options{Order: o, Layout: *name, Inner: *inner}
	if *shape {
		sh, err := kb.Shape(opts)
		if err != nil {
			return err
		}
		e := json.NewEncoder(os.Stdout)
		e.SetIndent("", " ")
		return e.Encode(sh)
	}
	g, err := kb.Map(opts)
	if err != nil {
		return err
	}
	header := []string{
		fmt.Sprintf("%s: %s index for each key, rows top to bottom.", kb.Name, *order),
		"Generated by keybars gen from QMK's " + src + ".",
	}
	if o == qmk.Keymapp {
		header = append(header, "Keymapp never lights index 0, so it is a gap.")
	}
	return g.Format(os.Stdout, header...)
}
