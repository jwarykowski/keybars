# keybars

A music equaliser on a [ZSA](https://www.zsa.io) keyboard's backlight. Whatever
the computer plays, the keyboard's 12 main columns become bars rising from the
bottom row, green through to red.

- Talks to Keymapp's API over one connection and sends only the keys that
  changed, so it keeps up with the music: about 30 fps on a Moonlander.
- When the music stops, the keys go back to your layout's colours; the same
  when keybars exits.
- The number row is left alone by default, free for other lights such as
  [herdr-zsa-lights](https://github.com/jwarykowski/herdr-zsa-lights)' agent
  status.

## Requirements

- A Moonlander or Voyager, and [Keymapp](https://www.zsa.io/flash) running
  with its **API enabled**
- [cava](https://github.com/karlstav/cava) to listen to the audio
  (`dnf install cava`, `apt install cava`, `brew install cava`; macOS also
  needs a loopback device such as [BlackHole](https://github.com/ExistentialAudio/BlackHole))
- Go 1.25+ to build

## Install

```sh
go install github.com/jwarykowski/keybars@latest
```

## Usage

```sh
keybars                      # Moonlander, bars 4 rows high
keybars -model voyager
keybars -height 5            # use the number row too
keybars -off layout          # unlit keys keep the layout's colours
keybars -colours '#0044ff,#aa00ff,#ff00aa'
keybars -reserve 36,61       # never touch these LEDs
```

Stop it with Ctrl-C. `-v` prints the frame rate on exit.

Any program can drive the bars instead of cava: `-stdin` reads cava's raw
ascii format, one frame per line, one height (0 to the bar height) per column:

```sh
printf '1;2;3;4;3;2;1;0;1;2;3;4;\n' | keybars -stdin
```

## Keyboard maps

LED indices follow ZSA's QMK `rgb_matrix` layout. The bars use the six main
columns of each half; the inner columns and thumb clusters are left out. The
Moonlander map is tested; the Voyager map is derived from the same layout and
untested, so corrections are welcome.

## Development

```sh
go test ./...
go vet ./...
```

## License

MIT
