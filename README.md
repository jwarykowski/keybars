# keybars

A music equaliser on a [ZSA](https://www.zsa.io) keyboard's backlight. Whatever
the computer plays, each column of keys becomes a bar rising from the bottom
row, green through to red, bass on the left and treble on the right.

- Talks to Keymapp's API over one connection and sends only the keys that
  changed, so it keeps up with the music: about 30 fps on a Moonlander.
- Finds the connected keyboard on its own; any other board needs only a text
  map, which `keybars find` helps you write.
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
keybars                      # the connected keyboard, bars 4 rows high
keybars -model voyager       # pick a built-in map
keybars -map my-board.txt    # or your own
keybars -height 5            # use the number row too
keybars -off layout          # unlit keys keep the layout's colours
keybars -colours '#0044ff,#aa00ff,#ff00aa'
keybars -reserve 36,61       # never touch these LEDs
keybars -smooth 15           # jumpier (0-100, default 35)
```

Stop it with Ctrl-C. `-v` prints the frame rate on exit.

Any program can drive the bars instead of cava: `-stdin` reads cava's raw
ascii format, one frame per line, one height (0 to the bar height) per column:

```sh
printf '1;2;3;4;3;2;1;0;1;2;3;4;\n' | keybars -stdin
```

## Keyboard maps

A map is the LED index under each key, drawn as the keyboard looks: one line
per row of keys, top to bottom. Columns become bars and rows become levels,
so any size works. `-` marks a gap, `|` is decoration, `#` starts a comment.
[`maps/moonlander.txt`](maps/moonlander.txt):

```
 0  5 10 15 20 25 | 61 56 51 46 41 36
 1  6 11 16 21 26 | 62 57 52 47 42 37
 2  7 12 17 22 27 | 63 58 53 48 43 38
 3  8 13 18 23 28 | 64 59 54 49 44 39
 4  9 14 19 24 29 | 65 60 55 50 45 40
```

`keybars maps` lists the built-in maps and which one the connected keyboard
uses (matched on the name Keymapp reports). The Moonlander map is tested; the
Voyager map is derived from ZSA's layout and untested, so corrections are
welcome.

### Adding a keyboard

1. `keybars find` darkens the board and lights one LED at a time, printing
   its index; press Enter for the next. Write each index where its key sits.
2. `keybars test -map my-board.txt` sweeps a bar across each column, then a
   level across each row: a key out of line is a wrong index.
3. Use it with `-map my-board.txt`, or open a pull request adding it to
   [`maps/`](maps) as `<model>.txt`, named so it appears in Keymapp's name
   for the keyboard (`ergodox.txt` for "ErgoDox EZ").

## Development

```sh
go test ./...
go vet ./...
```

## License

MIT
