<p align="center"><img src="assets/keybars.svg" alt="keybars" width="120"></p>

# keybars

A music equaliser on your keyboard's backlight. Whatever the computer plays, each column of keys becomes a bar rising from the bottom
row, bass on the left and treble on the right, with a white peak dot that
holds a moment and falls like a hi-fi meter.

- Sends only the keys that changed, so it keeps up with the music: about
  30 fps on a Moonlander.
- Drives keyboards through a small backend interface: ZSA's Keymapp
  (Moonlander, Voyager), or [OpenRGB](https://openrgb.org) for most other
  per-key RGB keyboards (Razer, Corsair, Logitech, SteelSeries, ASUS, QMK
  and more), whose key layout it reads from OpenRGB with no map to write.
- Finds the connected keyboard on its own; any other board needs only a text
  map, which `keybars find` helps you write.
- When the music stops, the keys go back to your layout's colours; the same
  when keybars exits. (On ZSA boards, keys outside the bars are dark while it
  plays: once Keymapp drives any key, the firmware shows only what it sets.)
- The number row is left alone by default, free for other lights such as
  [herdr-zsa-lights](https://github.com/jwarykowski/herdr-zsa-lights)' agent
  status.

## Requirements

- A keyboard with a backend:
  - ZSA Moonlander and Voyager through [Keymapp](https://www.zsa.io/flash),
    running with its **API enabled**, or
  - a per-key RGB keyboard OpenRGB supports, with OpenRGB running and its
    **SDK server started** (`openrgb --server`, or "Start Server" on its SDK
    Server tab)
- [cava](https://github.com/karlstav/cava) to listen to the audio
  (`dnf install cava`, `apt install cava`, `brew install cava`; macOS also
  needs a loopback device such as [BlackHole](https://github.com/ExistentialAudio/BlackHole):
  see [macOS audio](#macos-audio))
- Go 1.25+ to build from source

## Install

```sh
brew install jwarykowski/tap/keybars    # macOS or Linux; brings cava too
go install github.com/jwarykowski/keybars@latest   # or from source
```

Releases also have binaries for macOS and Linux on
[GitHub](https://github.com/jwarykowski/keybars/releases).

## Usage

```sh
keybars                      # the connected keyboard, bars up to the number row
keybars -model voyager       # pick a built-in map
keybars -map my-board.txt    # or your own
keybars gen zsa/voyager      # write a map from QMK's definition
keybars -height 5            # use the number row too
keybars -colours '#00ff00,#ffff00,#ff0000'   # meter colours, bottom to top
keybars -gain 2              # reach the top more often (default 1.5)
keybars -peaks=false         # no peak dots
keybars -reserve 36,61       # never touch these LEDs
keybars -smooth 15           # jumpier (0-100, default 35)
keybars -backend openrgb     # the first keyboard OpenRGB has
keybars -backend openrgb -device apex   # or the one whose name contains apex
keybars -source 'BlackHole 2ch'   # cava capture device (macOS needs a loopback)
keybars version              # the installed version
```

Stop it with Ctrl-C. `-v` prints the frame rate on exit.

### Config file

`keybars` and `keybars start` read their settings from
`~/.config/keybars/config` (`$XDG_CONFIG_HOME/keybars/config` when that is
set), one flag per line by its own name. Flags on the command line override
it, and `-config FILE` reads another file.

```
# ~/.config/keybars/config
source = BlackHole 2ch
colours = #00ff00,#ffff00,#ff0000
gain = 2
peaks = false
```

Lines starting with `#` are comments; a `#` later in a line is part of the
value, as in the colours. An unknown setting stops keybars with its line
number, except keybars Pro's own (`low-hz`, `high-hz`, `brightness`,
`symmetry`), which it skips so the same file works in both.

### Running in the background

```sh
keybars start                # run in the background with the config file's settings
keybars start -gain 3        # flags work here too
keybars status               # running or not, and where the log is
keybars stop                 # hand the keys back to the layout and exit
```

`start` keeps a pid file and log in `~/.local/state/keybars/`
(`$XDG_STATE_HOME/keybars/`). If keybars fails straight away, say Keymapp
isn't running, `start` prints the error instead.

To start it at login on macOS, save this as
`~/Library/LaunchAgents/dev.jwarykowski.keybars.plist`, with the path to your
keybars:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>dev.jwarykowski.keybars</string>
  <key>ProgramArguments</key>
  <array><string>/Users/you/go/bin/keybars</string></array>
  <key>EnvironmentVariables</key>
  <dict><key>PATH</key><string>/opt/homebrew/bin:/usr/bin:/bin</string></dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>StandardErrorPath</key><string>/tmp/keybars.log</string>
</dict>
</plist>
```

Load it with `launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/dev.jwarykowski.keybars.plist`.
keybars exits when Keymapp or the keyboard goes away, and `KeepAlive` starts
it again every 10 seconds until they're back, so set Keymapp to open at login
too. `PATH` is there so launchd finds cava.

On Linux, a systemd user unit does the same, in
`~/.config/systemd/user/keybars.service`:

```ini
[Unit]
Description=keybars

[Service]
ExecStart=%h/go/bin/keybars
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
```

Then `systemctl --user enable --now keybars`.

Any program can drive the bars instead of cava: `-stdin` reads cava's raw
ascii format, one frame per line, one height (0 to the bar height) per column:

```sh
printf '1;2;3;4;3;2;1;0;1;2;3;4;\n' | keybars -stdin
```

## OpenRGB

With `-backend openrgb`, keybars connects to OpenRGB's SDK server
(`-openrgb`, default `127.0.0.1:6742`), takes the keyboard into its per-LED
mode while music plays, and puts its previous mode back afterwards. Keys
outside the bars keep their colours if the keyboard was already per-LED, and
go dark otherwise.

Most keyboards in OpenRGB come with a key matrix, which keybars uses as the
map: the top row (usually Esc and F-keys) is left free by default, like the
number row on ZSA boards. `keybars maps -backend openrgb` lists the devices
and prints that map, to save and edit for `-map`. For a keyboard without a
matrix, `keybars find -backend openrgb` lights LEDs from index 0 with their
OpenRGB names. When OpenRGB rescans its devices, or the keyboard is
unplugged and plugged back in, keybars finds it again and carries on. The
OpenRGB backend is tested against a simulated server, not yet a real
keyboard, so reports are welcome.

## macOS audio

macOS gives programs no way to listen to what it plays, so cava listens to a
loopback device instead:

1. Install [BlackHole](https://github.com/ExistentialAudio/BlackHole)
   (`brew install blackhole-2ch`).
2. In Audio MIDI Setup, add a Multi-Output Device with **BlackHole 2ch** and
   your speakers or headphones. Make the speakers or headphones the primary
   device and tick Drift Correction for BlackHole.
3. Choose the Multi-Output Device as the sound output (Control Centre >
   Sound). Connecting Bluetooth headphones switches the output back to them,
   so pick it again after. The volume keys don't work on a Multi-Output
   Device; use the headphones' or speakers' own.
4. Run `keybars -source 'BlackHole 2ch'`, or put `source = BlackHole 2ch` in
   the config file.

If the bars stay dark:

- `keybars test` lights the keys without audio, so it shows whether Keymapp
  is the problem.
- macOS counts listening to BlackHole as using the microphone. Allow your
  terminal (or keybars, under launchd) in System Settings > Privacy &
  Security > Microphone; without it cava hears silence and says nothing.
- In QuickTime, File > New Audio Recording with BlackHole 2ch as the input
  shows whether sound reaches BlackHole at all.
- A cava config with `method = portaudio` and `source = list` under
  `[input]` lists the devices cava can see, by the names `-source` takes.

## Keyboard maps

A map is the backend's index for each key (Keymapp's, or OpenRGB's), drawn as the keyboard looks: one line
per row of keys, top to bottom. Keymapp numbers each half on its own, row by
row from the left, inner column included (not the firmware's LED order).
Columns become bars and rows become levels, so any size works. `-` marks a gap, `|` is decoration, `#` starts a comment.
[`maps/moonlander.txt`](maps/moonlander.txt):

```
 -  1  2  3  4  5 | 37 38 39 40 41 42
 7  8  9 10 11 12 | 44 45 46 47 48 49
14 15 16 17 18 19 | 51 52 53 54 55 56
21 22 23 24 25 26 | 57 58 59 60 61 62
27 28 29 30 31  - |  - 63 64 65 66 67
```

`keybars maps` lists the built-in maps and which one the connected keyboard
uses (matched on the name Keymapp reports). The Moonlander and Voyager maps
are tested on real boards, and `keybars gen` reproduces both from QMK.

### Adding a keyboard

If QMK knows the keyboard, `keybars gen` writes its map from QMK's
definition in [qmk_firmware/keyboards](https://github.com/qmk/qmk_firmware/tree/master/keyboards):

```sh
keybars gen zsa/voyager > voyager.txt     # fetched from qmk_firmware
keybars gen path/to/keyboard.json         # or a local copy
keybars gen -order firmware zsa/moonlander  # QMK's own LED order
```

Keymapp's index is not the firmware's LED order: it numbers each half row by
row, thumbs last, while the Moonlander's firmware runs down each column. `gen`
follows Keymapp by default; `-order firmware` gives QMK's `rgb_matrix`
order, for backends that talk to the firmware directly. The thumb cluster
and a split half's short inner column are left out (`-inner` keeps the
latter). Definitions that inherit from a parent `info.json` aren't followed
yet, so pass a merged file (`qmk info -kb <keyboard> -f json`) for those.
Check the result with `keybars test` as below.

Otherwise, find the indices by hand:

1. `keybars find` darkens the board and lights one key at a time, printing
   its index (Keymapp never lights index 0, so start from 1); press Enter for the next. Write each index where its key sits.
2. `keybars test -map my-board.txt` sweeps a bar across each column, then a
   level across each row: a key out of line is a wrong index.
3. Use it with `-map my-board.txt`, or open a pull request adding it to
   [`maps/`](maps) as `<model>.txt`, named so it appears in Keymapp's name
   for the keyboard (`ergodox.txt` for "ErgoDox EZ").

## Development

```sh
go test ./...
go vet ./...
golangci-lint run ./...   # .golangci.yml; golangci-lint fmt ./... formats
```

Pushing a `v*` tag runs GoReleaser (`.goreleaser.yaml`): it builds the
binaries, publishes the GitHub release and opens a pull request on
[jwarykowski/homebrew-tap](https://github.com/jwarykowski/homebrew-tap) with
the formula. It needs a `GH_RELEASE_TOKEN` secret with write access to the tap.

## Support

keybars is free and MIT licensed. If it makes your desk a bit more fun, you
can [sponsor its development](https://github.com/sponsors/jwarykowski).

## License

MIT. The code keybars includes from other projects keeps its own licences,
listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) and printed by
`keybars licences`; `scripts/notices.sh` regenerates the file after a
dependency changes.
