// keybars: a music equaliser on a ZSA keyboard's backlight. cava listens to
// what the system plays; each column of keys becomes a bar rising from the
// bottom row. Keys go back to the layout's colours when the music stops and
// when keybars exits.
package main

import (
	"bufio"
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jwarykowski/keybars/internal/bars"
	"github.com/jwarykowski/keybars/internal/keymapp"
	"github.com/jwarykowski/keybars/internal/layout"
	"github.com/jwarykowski/keybars/internal/source"
)

//go:embed maps/*.txt
var embedded embed.FS

// maps holds the built-in keyboard maps, maps/<model>.txt.
var maps, _ = fs.Sub(embedded, "maps")

const usage = `keybars: a music equaliser on a ZSA keyboard's backlight

usage:
  keybars [flags]         show the music playing as bars (needs cava)
  keybars test [flags]    sweep the bars across the keyboard to check a map
  keybars find [flags]    light LEDs one at a time to write a map
  keybars maps            list the built-in maps and the connected keyboard

Run a command with -h for its flags.
`

func main() {
	cmd, args := "run", os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "run":
		err = run(args)
	case "test":
		err = test(args)
	case "find":
		err = find(args)
	case "maps":
		err = listMaps(args)
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

// common are the flags every command takes.
type common struct {
	model, mapFile, socket *string
}

func commonFlags(fl *flag.FlagSet) common {
	return common{
		model:   fl.String("model", "", "built-in map: "+strings.Join(layout.Builtin(maps), ", ")+" (default: detect the connected keyboard)"),
		mapFile: fl.String("map", "", "map file to use instead of a built-in one"),
		socket:  fl.String("socket", "", "Keymapp socket (default: $KEYMAPP_SOCKET, else Keymapp's own)"),
	}
}

func connect(socket string) (*keymapp.Client, error) {
	path := socket
	if path == "" {
		var err error
		if path, err = keymapp.SocketPath(); err != nil {
			return nil, err
		}
	}
	return keymapp.Dial(path)
}

// loadMap reads -map, else the -model map, else the map matching the
// keyboard Keymapp has connected.
func loadMap(c common, client *keymapp.Client) (layout.Map, error) {
	if *c.mapFile != "" {
		f, err := os.Open(*c.mapFile)
		if err != nil {
			return layout.Map{}, err
		}
		defer f.Close()
		return layout.Parse(*c.mapFile, f)
	}
	model := *c.model
	if model == "" {
		kb, err := client.Keyboard()
		if err != nil {
			return layout.Map{}, err
		}
		var ok bool
		if model, ok = layout.Detect(maps, kb); !ok {
			return layout.Map{}, fmt.Errorf("no built-in map for %q: write one with `keybars find` and pass -map FILE", kb)
		}
	}
	return layout.Load(maps, model)
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
	idle := fl.Duration("idle", time.Second, "silence before the keys go back to the layout")
	refresh := fl.Duration("refresh", 2*time.Second, "resend every key this often, in case something reset the board")
	stdin := fl.Bool("stdin", false, "read frames (cava raw ascii, \"3;5;1;...\") from stdin instead of running cava")
	peaks := fl.Bool("peaks", true, "falling peak dots above the bars (-peaks=false to hide)")
	peakColour := fl.String("peak-colour", "#ffffff", "peak dot colour")
	peakHold := fl.Duration("peak-hold", 400*time.Millisecond, "how long a peak dot holds before falling")
	peakFall := fl.Float64("peak-fall", 6, "how fast peak dots fall, in rows per second")
	verbose := fl.Bool("v", false, "print frames painted and LEDs sent on exit")
	if err := fl.Parse(args); err != nil {
		return err
	}

	cs, err := bars.ParseColours(*colours)
	if err != nil {
		return err
	}
	var reserved []int
	for _, s := range strings.Split(*reserve, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("-reserve: %w", err)
		}
		reserved = append(reserved, n)
	}

	client, err := connect(*c.socket)
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
			srcErr <- source.Cava(ctx, grid.Columns(), grid.Height*fine, *fps, *smooth, raw)
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
	if err == nil {
		select {
		case err = <-srcErr:
		default:
		}
	}
	if err == io.EOF {
		err = nil
	}
	return err
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
	client, err := connect(*c.socket)
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
	defer r.Clear()

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
	for x := 0; x < grid.Columns(); x++ {
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
	socket := fl.String("socket", "", "Keymapp socket (default: $KEYMAPP_SOCKET, else Keymapp's own)")
	from := fl.Int("from", 1, "first key index (Keymapp never lights 0)")
	to := fl.Int("to", 0, "last key index (default: until the keyboard has no more)")
	auto := fl.Duration("auto", 0, "step on a timer instead of waiting for Enter")
	if err := fl.Parse(args); err != nil {
		return err
	}
	client, err := connect(*socket)
	if err != nil {
		return err
	}
	defer client.Close()
	if kb, err := client.Keyboard(); err == nil {
		fmt.Printf("%s: every key goes dark, then one lights at a time.\n", kb)
	}
	if *auto == 0 {
		fmt.Println("Note the key under each LED number; Enter for the next, q to stop.")
	}
	white := keymapp.Colour{R: 255, G: 255, B: 255}
	if err := client.SetAll(keymapp.Colour{}); err != nil {
		return err
	}
	defer client.RestoreAll()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	in := bufio.NewReader(os.Stdin)
	for led := *from; *to == 0 || led <= *to; led++ {
		if err := client.Set(led, white); err != nil {
			fmt.Printf("no LED %d: done\n", led)
			return nil
		}
		fmt.Printf("LED %d ", led)
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
		if err := client.Set(led, keymapp.Colour{}); err != nil {
			return err
		}
	}
	return nil
}

func listMaps(args []string) error {
	fl := flag.NewFlagSet("keybars maps", flag.ContinueOnError)
	socket := fl.String("socket", "", "Keymapp socket (default: $KEYMAPP_SOCKET, else Keymapp's own)")
	if err := fl.Parse(args); err != nil {
		return err
	}
	for _, n := range layout.Builtin(maps) {
		m, err := layout.Load(maps, n)
		if err != nil {
			return err
		}
		fmt.Printf("%-12s %d columns x %d rows\n", n, m.Columns(), m.Rows())
	}
	client, err := connect(*socket)
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
