package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jwarykowski/keybars/pkg/layout"
	"github.com/jwarykowski/keybars/pkg/openrgb"
	"github.com/jwarykowski/keybars/pkg/qmk"
	"github.com/jwarykowski/keybars/pkg/rgb"
)

const findHelp = `Enter      next LED (not a key, or not noting it)
ROW COL    this LED is the key at ROW, COL, counting from 1 at the top left;
           a wide key covers columns: 5 4-7
b          back one LED
g N        go to LED N
q          stop (Ctrl-C too)`

var (
	white = rgb.Colour{R: 255, G: 255, B: 255}
	// an LED already passed, so the board shows how far it's got
	done = rgb.Colour{G: 40}
)

// find lights LEDs one at a time and prints each index. Told where each
// lit key sits, it writes the map itself.
func find(args []string) error {
	fl := flag.NewFlagSet("keybars find", flag.ContinueOnError)
	cf := connFlags(fl)
	from := fl.Int("from", -1, "first key index (default: 1 on Keymapp, which never lights 0, else 0)")
	to := fl.Int("to", -1, "last key index (default: until the keyboard has no more)")
	auto := fl.Duration("auto", 0, "step on a timer instead of waiting for Enter")
	out := fl.String("o", "", "write the map to this file (default: print it), once keys have places")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if err := noArgs(fl); err != nil {
		return err
	}
	if err := cf.check(); err != nil {
		return err
	}
	if *from < -1 || *to < -1 {
		return fmt.Errorf("-from %d -to %d: want LED indices, 0 or more", *from, *to)
	}
	if *from >= 0 && *to >= 0 && *to < *from {
		return fmt.Errorf("-to %d is before -from %d", *to, *from)
	}
	if *auto < 0 {
		return fmt.Errorf("-auto %v: want a positive time, e.g. 2s", *auto)
	}
	if err := notInBackground(); err != nil {
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
	kb, err := client.Keyboard()
	if err == nil {
		fmt.Printf("%s: every key goes dark, then one lights at a time.\n", kb)
		if m, ok := layout.Detect(maps, kb); ok && *cf.backend == "keymapp" {
			fmt.Printf("keybars has a built-in map for it (-model %s), so there's nothing to find: keybars test shows it.\n", m)
		}
	}
	// OpenRGB says how many LEDs there are
	last := -1
	if d, ok := client.(*openrgb.Device); ok {
		last = len(d.Controller().LEDs) - 1
	}
	if *auto == 0 {
		fmt.Println("For each lit LED: Enter for the next, or the key's ROW COL (e.g. 2 5) to write the map as you go; ? for more.")
	}
	named, _ := client.(interface{ Name(led int) string })
	if err := client.SetAll(rgb.Colour{}); err != nil {
		return err
	}
	defer func() { _ = client.RestoreAll() }() // best effort on the way out

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var lines <-chan string
	if *auto == 0 {
		lines = readLines(ctx)
	}

	set := func(led int, c rgb.Colour) error {
		if err := client.Set(led, c); err != nil {
			return err
		}
		return rgb.Flush(client)
	}
	f := finder{at: map[[2]int]int{}, passed: map[int]bool{}}
	led, shown := *from, -1
	for *to < 0 || led <= *to {
		// asked again after help, or a line it didn't follow, it's already lit
		if led != shown {
			ok, err := light(client, set, led)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Printf("no LED %d: done\n", led)
				break
			}
			shown = led
		}
		fmt.Printf("LED %d", led)
		if named != nil {
			fmt.Printf(" (%s)", named.Name(led))
		}
		if last >= 0 {
			fmt.Printf(", %d of %d", led-*from+1, last-*from+1)
		}
		next, quit := led+1, false
		if *auto > 0 {
			fmt.Println()
			select {
			case <-time.After(*auto):
			case <-ctx.Done():
				quit = true
			}
		} else {
			fmt.Print(" > ")
			line, ok := "", false
			select {
			case line, ok = <-lines:
			case <-ctx.Done():
			}
			if !ok {
				fmt.Println()
				quit = true
			} else {
				c, err := parseFind(line)
				switch {
				case err != nil:
					fmt.Println(err)
					continue
				case c.op == 'q':
					quit = true
				case c.op == '?':
					fmt.Println(findHelp)
					continue
				case c.op == 'b':
					next = max(led-1, *from)
					f.forget(next)
				case c.op == 'g':
					next = c.led
				case c.op == 'p':
					f.place(led, c.row, c.cols)
				}
			}
		}
		if quit {
			break
		}
		if next > led {
			f.passed[led] = true
		}
		if next != led {
			// passed shows dim; one gone back over is dark again
			c := rgb.Colour{}
			if f.passed[led] {
				c = done
			}
			if err := set(led, c); err != nil {
				return err
			}
		}
		led = next
	}
	return f.write(*out, kb)
}

// light lights led white, reporting false past the keyboard's last LED.
// Keymapp fails a call now and then, so it tries once more before deciding.
func light(kb keyboard, set func(int, rgb.Colour) error, led int) (bool, error) {
	err := set(led, white)
	if err != nil && !errors.Is(err, openrgb.ErrNoLED) {
		time.Sleep(200 * time.Millisecond)
		err = set(led, white)
	}
	if err == nil {
		return true, nil
	}
	if pastLast(kb, err) {
		return false, nil
	}
	return false, err
}

// finder is what find has been told: the LED at each key's place, and the
// LEDs it has passed.
type finder struct {
	at     map[[2]int]int // [row, col] -> LED
	passed map[int]bool
}

// place puts led at row, in each of cols, wherever it was before.
func (f *finder) place(led, row int, cols []int) {
	f.forget(led)
	for _, c := range cols {
		f.at[[2]int{row, c}] = led
	}
}

// forget takes led out of the map and the LEDs passed.
func (f *finder) forget(led int) {
	for k, v := range f.at {
		if v == led {
			delete(f.at, k)
		}
	}
	delete(f.passed, led)
}

// write saves the map to path, or prints it without one; nothing when no
// key has a place.
func (f *finder) write(path, kb string) error {
	if len(f.at) == 0 {
		return nil
	}
	rows, cols := 0, 0
	for k := range f.at {
		rows, cols = max(rows, k[0]+1), max(cols, k[1]+1)
	}
	g := qmk.Grid{Cells: make([][]int, rows)}
	for r := range g.Cells {
		g.Cells[r] = make([]int, cols)
		for c := range g.Cells[r] {
			led, ok := f.at[[2]int{r, c}]
			if !ok {
				led = -1
			}
			g.Cells[r][c] = led
		}
	}
	header := []string{"written by keybars find"}
	if kb != "" {
		header[0] = kb + ": " + header[0]
	}
	if path == "" {
		fmt.Println("\nThe map, to save and pass as -map FILE (or run with -o FILE):")
		return g.Format(os.Stdout, header...)
	}
	var b strings.Builder
	if err := g.Format(&b, header...); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return err
	}
	fmt.Printf("Wrote %s (%d keys). Check it: keybars test -map %s\n", path, len(f.at), path)
	return nil
}

// findCmd is a line typed at find's prompt.
type findCmd struct {
	op   byte // 'n' next, 'p' place, 'b' back, 'g' go to led, 'q' quit, '?' help
	led  int
	row  int   // from 0
	cols []int // from 0
}

func parseFind(line string) (findCmd, error) {
	f := strings.Fields(strings.ToLower(line))
	num := func(s string) (int, bool) {
		n, err := strconv.Atoi(s)
		return n, err == nil
	}
	switch {
	case len(f) == 0:
		return findCmd{op: 'n'}, nil
	case len(f) == 1 && (f[0] == "q" || f[0] == "b" || f[0] == "?"):
		return findCmd{op: f[0][0]}, nil
	case len(f) == 1 && (f[0] == "h" || f[0] == "help"):
		return findCmd{op: '?'}, nil
	case len(f) == 2 && f[0] == "g":
		if n, ok := num(f[1]); ok && n >= 0 {
			return findCmd{op: 'g', led: n}, nil
		}
	case len(f) == 2:
		row, ok := num(f[0])
		lo, hi, ranged := strings.Cut(f[1], "-")
		a, okA := num(lo)
		b := a
		okB := true
		if ranged {
			b, okB = num(hi)
		}
		if ok && okA && okB && row >= 1 && a >= 1 && b >= a {
			c := findCmd{op: 'p', row: row - 1}
			for x := a; x <= b; x++ {
				c.cols = append(c.cols, x-1)
			}
			return c, nil
		}
	}
	return findCmd{}, fmt.Errorf("%q? Enter for the next LED, ROW COL to place this one (e.g. 2 5), ? for more", strings.TrimSpace(line))
}

// readLines sends each line typed on stdin until it ends or ctx is done.
func readLines(ctx context.Context) <-chan string {
	lines := make(chan string)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			select {
			case lines <- sc.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	return lines
}

// notInBackground refuses while a background keybars is drawing: it would
// paint over the one lit key.
func notInBackground() error {
	// without a home there's no pid file, so nothing in the background
	if dir, err := stateDir(); err == nil {
		if pid, ok := running(dir); ok {
			return fmt.Errorf("keybars is running in the background (pid %d), drawing over the keys: keybars stop first", pid)
		}
	}
	return nil
}

// pastLast reports whether err from lighting an LED means the keyboard has
// no more, rather than that it has gone: Keymapp says only that the call
// failed, so it asks whether the keyboard is still there.
func pastLast(kb keyboard, err error) bool {
	if errors.Is(err, openrgb.ErrNoLED) {
		return true
	}
	if _, ok := kb.(*openrgb.Device); ok {
		return false
	}
	_, kerr := kb.Keyboard()
	return kerr == nil
}
