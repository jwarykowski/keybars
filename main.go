// keybars: a music equaliser on a keyboard's backlight. cava listens to
// what the system plays; each column of keys becomes a bar rising from the
// bottom row. Keys go back to the layout's colours when the music stops and
// when keybars exits.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
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

func main() {
	cmd, args := "run", os.Args[1:]
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
		os.Exit(2)
	}
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, "keybars:", err)
		os.Exit(1)
	}
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

// loadMap reads -map, else the -model map, else the map for the connected
// keyboard: the built-in one matching Keymapp's name for it, or an OpenRGB
// device's own key matrix.
func loadMap(c common, kb keyboard) (layout.Map, error) {
	if *c.mapFile != "" {
		f, err := os.Open(*c.mapFile)
		if err != nil {
			return layout.Map{}, err
		}
		defer f.Close()
		return layout.Parse(*c.mapFile, f)
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

// openrgbMap is the map from an OpenRGB device's key matrix.
func openrgbMap(d *openrgb.Device) (layout.Map, error) {
	name, _ := d.Keyboard()
	cells, ok := d.Map()
	if !ok {
		// QMK's map for the board, in the firmware's LED order, which is
		// OpenRGB's for a QMK board that talks to it directly
		if b, ok := builtin.MatchQMK(name); ok {
			fmt.Fprintf(os.Stderr, "keybars: OpenRGB gives no key matrix for %s; using the map from QMK (untested on this board: keybars test checks it)\n", name)
			return layout.Load(builtin.QMK, b.ID)
		}
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

	if err := checkRun(*fps, *smooth, *gain); err != nil {
		return err
	}
	cs, err := bars.ParseColours(*colours)
	if err != nil {
		return err
	}
	var reserved []int
	for s := range strings.SplitSeq(*reserve, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("-reserve: %w", err)
		}
		reserved = append(reserved, n)
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
		pc, err := bars.ParseColours(*peakColour)
		if err != nil {
			return fmt.Errorf("-peak-colour: %w", err)
		}
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
	case gain <= 0:
		return fmt.Errorf("-gain %g: want more than 0", gain)
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
	show := func(heights []int) bool {
		if _, err := r.Frame(heights, nil); err != nil {
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
			return nil
		}
	}
	for level := 1; level <= grid.Height; level++ {
		h := make([]int, grid.Columns())
		for x := range h {
			h[x] = level
		}
		if !show(h) {
			return nil
		}
	}
	return nil
}

// find lights LEDs one at a time and prints each index, to draw a map from.
func find(args []string) error {
	fl := flag.NewFlagSet("keybars find", flag.ContinueOnError)
	cf := connFlags(fl)
	from := fl.Int("from", -1, "first key index (default: 1 on Keymapp, which never lights 0, else 0)")
	to := fl.Int("to", -1, "last key index (default: until the keyboard has no more)")
	auto := fl.Duration("auto", 0, "step on a timer instead of waiting for Enter")
	if err := fl.Parse(args); err != nil {
		return err
	}
	client, err := cf.connect()
	if err != nil {
		return err
	}
	defer client.Close()
	if *from < 0 {
		*from = 0
		if *cf.backend == "keymapp" {
			*from = 1
		}
	}
	if kb, err := client.Keyboard(); err == nil {
		fmt.Printf("%s: every key goes dark, then one lights at a time.\n", kb)
	}
	if *auto == 0 {
		fmt.Println("Note the key under each LED number; Enter for the next, q to stop.")
	}
	// set colours one LED and shows it at once, on backends that buffer.
	set := func(led int, c rgb.Colour) error {
		if err := client.Set(led, c); err != nil {
			return err
		}
		return rgb.Flush(client)
	}
	named, _ := client.(interface{ Name(led int) string })
	white := rgb.Colour{R: 255, G: 255, B: 255}
	if err := client.SetAll(rgb.Colour{}); err != nil {
		return err
	}
	defer func() { _ = client.RestoreAll() }() // best effort on the way out

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	in := bufio.NewReader(os.Stdin)
	for led := *from; *to < 0 || led <= *to; led++ {
		if err := set(led, white); err != nil {
			fmt.Printf("no LED %d: done\n", led)
			return nil
		}
		fmt.Printf("LED %d ", led)
		if named != nil {
			fmt.Printf("(%s) ", named.Name(led))
		}
		if *auto > 0 {
			fmt.Println()
			select {
			case <-time.After(*auto):
			case <-ctx.Done():
				return nil
			}
		} else {
			line, err := in.ReadString('\n')
			if err != nil || strings.TrimSpace(line) == "q" {
				return nil
			}
		}
		if err := set(led, rgb.Colour{}); err != nil {
			return err
		}
	}
	return nil
}

func listMaps(args []string) error {
	fl := flag.NewFlagSet("keybars maps", flag.ContinueOnError)
	cf := connFlags(fl)
	if err := fl.Parse(args); err != nil {
		return err
	}
	if *cf.backend == "openrgb" {
		return listOpenRGB(cf)
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

// listOpenRGB lists OpenRGB's devices, then prints the map keybars would use
// for the chosen one, ready to save and edit for -map.
func listOpenRGB(cf conn) error {
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

// qmkRaw is where gen fetches keyboard definitions named rather than given
// as a file.
const (
	qmkAPI = "https://keyboards.qmk.fm/v1/keyboards/"
	qmkRaw = "https://raw.githubusercontent.com/qmk/qmk_firmware/master/keyboards/"
)

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
	kb, err := readQMK(src)
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

// qmkClient fetches keyboard definitions; the timeout keeps gen from hanging.
var qmkClient = &http.Client{Timeout: 15 * time.Second}

func readQMK(src string) (qmk.Keyboard, error) {
	f, err := os.Open(src)
	if err == nil {
		defer f.Close()
		return qmk.Parse(f)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return qmk.Keyboard{}, err
	}
	var errs []error
	// QMK's API has the merged definition, which variants inherit; the raw
	// files are a fallback
	urls := []string{qmkAPI + strings.Trim(src, "/") + "/info.json"}
	for _, file := range []string{"keyboard.json", "info.json"} {
		urls = append(urls, qmkRaw+strings.Trim(src, "/")+"/"+file)
	}
	for _, url := range urls {
		resp, err := qmkClient.Get(url)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			errs = append(errs, fmt.Errorf("%s: %s", url, resp.Status))
			continue
		}
		kb, err := qmk.Parse(resp.Body)
		resp.Body.Close()
		return kb, err
	}
	return qmk.Keyboard{}, fmt.Errorf("%s is not a file or a QMK keyboard: %w", src, errors.Join(errs...))
}
