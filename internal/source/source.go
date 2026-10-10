// Package source reads bar heights: from cava, which listens to what the
// system is playing, or from any program writing cava's raw ascii format
// ("3;5;1;...;" per line).
package source

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Parse reads one line of cava raw ascii output: heights separated by ';'.
func Parse(line string) ([]int, error) {
	var out []int
	for _, f := range strings.Split(strings.TrimSpace(line), ";") {
		if f == "" {
			continue
		}
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil, fmt.Errorf("bad frame %q: %w", line, err)
		}
		out = append(out, n)
	}
	return out, nil
}

// Read sends each frame from r to frames until r ends, then closes frames.
func Read(r io.Reader, frames chan<- []int) error {
	defer close(frames)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f, err := Parse(sc.Text())
		if err != nil {
			return err
		}
		frames <- f
	}
	return sc.Err()
}

// cavaConfig asks for one bar per grid column, scaled to the grid height,
// as ascii lines on stdout. Mono runs bass to treble left to right; cava's
// default stereo mirrors the channels with the bass in the middle.
const cavaConfig = `[general]
bars = %d
framerate = %d

[output]
method = raw
raw_target = /dev/stdout
data_format = ascii
ascii_max_range = %d
bar_delimiter = 59
frame_delimiter = 10
channels = mono
mono_option = average

[smoothing]
noise_reduction = %d
`

// Cava runs cava with a generated config for bars columns of up to height,
// and streams its frames until ctx ends or cava exits. smooth is cava's
// noise_reduction, 0-100: lower is jumpier, higher is slower and smoother.
func Cava(ctx context.Context, bars, height, fps, smooth int, frames chan<- []int) error {
	f, err := os.CreateTemp("", "keybars-cava-*.conf")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := fmt.Fprintf(f, cavaConfig, bars, fps, height, smooth); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "cava", "-p", f.Name())
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start cava: %w (is it installed?)", err)
	}
	readErr := Read(out, frames)
	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("cava: %w", err)
	}
	return readErr
}
