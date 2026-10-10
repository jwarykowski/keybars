// keybars: a music equaliser on a ZSA keyboard's backlight. cava listens to
// what the system plays; each of the 12 main columns becomes a bar rising
// from the bottom row. Keys go back to the layout's colours when the music
// stops and when keybars exits.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
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

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "keybars:", err)
		os.Exit(1)
	}
}

func run() error {
	model := flag.String("model", "moonlander", "keyboard: "+strings.Join(layout.Models(), ", "))
	height := flag.Int("height", 0, "bar height in rows from the bottom; 0 = all but the number row")
	colours := flag.String("colours", "#00cc44,#88dd00,#ffcc00,#ff6600,#ff0000", "bar colours, bottom to top")
	off := flag.String("off", "dark", "unlit keys: dark, or layout (keep the layout's colours)")
	reserve := flag.String("reserve", "", "comma-separated LEDs never to touch")
	fps := flag.Int("fps", 30, "frames per second asked of cava")
	idle := flag.Duration("idle", time.Second, "silence before the keys go back to the layout")
	stdin := flag.Bool("stdin", false, "read frames (cava raw ascii, \"3;5;1;...\") from stdin instead of running cava")
	socket := flag.String("socket", "", "Keymapp socket (default: $KEYMAPP_SOCKET, else Keymapp's own)")
	verbose := flag.Bool("v", false, "print frames painted and LEDs sent on exit")
	flag.Parse()

	grid, err := layout.New(*model, *height)
	if err != nil {
		return err
	}
	cs, err := bars.ParseColours(*colours)
	if err != nil {
		return err
	}
	offMode := bars.Dark
	switch *off {
	case "dark":
	case "layout":
		offMode = bars.Layout
	default:
		return fmt.Errorf("-off %q: want dark or layout", *off)
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

	path := *socket
	if path == "" {
		if path, err = keymapp.SocketPath(); err != nil {
			return err
		}
	}
	client, err := keymapp.Dial(path)
	if err != nil {
		return err
	}
	defer client.Close()
	r := bars.New(grid, client, cs, offMode, reserved)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	frames := make(chan []int)
	srcErr := make(chan error, 1)
	go func() {
		if *stdin {
			srcErr <- source.Read(os.Stdin, frames)
		} else {
			srcErr <- source.Cava(ctx, grid.Height, *fps, frames)
		}
	}()

	var st stats
	start := time.Now()
	err = paint(ctx, r, latest(ctx, frames), *idle, &st)
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
// ctx is done. After idle of silence it hands the keys back to the layout.
func paint(ctx context.Context, r *bars.Renderer, next func() ([]int, bool), idle time.Duration, st *stats) error {
	var quietSince time.Time
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
		n, err := r.Frame(f)
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
