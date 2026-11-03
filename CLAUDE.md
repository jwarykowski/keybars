# keybars

The free, MIT-licensed CLI: music as bars on a per-key RGB keyboard, through
cava, with ZSA boards over Keymapp's API and others through OpenRGB. Its
packages (`pkg/`, `maps/`) are also imported by keybars Pro, a separate paid
app. README.md has the detail; this file is what to keep in mind while
working here.

## Commands

- `go vet ./...`, `go test -race ./...` (CI races too), with
  `GOTOOLCHAIN=auto`.
- CI fails if total coverage (`-coverpkg=./...`, so a package's tests count
  for the others they drive) drops below 94.5%; new code comes with tests.
- Tests must not need a keyboard: the commands' tests drive a fake Keymapp
  socket (`hardware_test.go`) and a fake OpenRGB server (`openrgb_test.go`).
  `start`'s tests run the test binary itself as keybars (`TestMain`), with
  a fake `cava` on PATH.
- `scripts/openrgb-check.sh` runs `TestLiveOpenRGB` against a real OpenRGB
  server's debug keyboards (needs OpenRGB; `OPENRGB=path`). Run it after
  changing the OpenRGB backend or map choice.
- Device descriptions saved with `keybars maps -backend openrgb -save` go in
  `pkg/openrgb/testdata/devices/` (`<index>-<name>.v<protocol>.bin`), where
  `TestDevices` reads them.
- After any dependency change: `scripts/notices.sh`. CI fails if
  THIRD_PARTY_NOTICES.md is stale (Dependabot PRs need this too).
- `scripts/maps.sh` regenerates the QMK maps and `maps/shapes.json` from
  QMK's API. keybars-www copies shapes.json with `make boards`.
- Keep `pkg/` free of anything Pro-only: nothing moves from this repo into
  Pro, and the CLI stays free.

## Issues and pull requests

- Issues and outside pull requests come in on the GitHub mirror (Forgejo
  has registration off). Apply a GitHub PR on Forgejo with its author kept
  (`git am` of the PR's `.patch`), then close it on GitHub with a link;
  never merge on GitHub, since the next mirror push overwrites it.

## Releasing

- Push an annotated `vX.Y.Z` tag on `main`. The release workflow runs on the
  tag (a manual run needs the tag to exist already), publishes to GitHub
  through GoReleaser, and opens a PR on `jwarykowski/homebrew-tap` with the
  new formula. Merge that PR (`gh pr merge -R jwarykowski/homebrew-tap`) or
  `brew` stays on the old version.
- Then bump keybars-pro (`go get github.com/jwarykowski/keybars@vX.Y.Z`,
  `make notices`) and add the release to keybars-www's
  `data/changelog.yaml`.

## Maps

- Keymapp's LED order differs from the firmware's: this CLI uses Keymapp's;
  keybars Pro drives firmware order and keeps its own maps for that.
- On OpenRGB, a board in `maps/qmk/boards.json` gets keybars' map over
  OpenRGB's matrix when its LED count matches (counts come from
  `shapes.json`, which `scripts/maps.sh` writes). `TestBarsStandStraight`
  checks every map's bars against the board's shape.
- A map that can't be verified (ErgoDox EZ: thumbs not on their own matrix
  row) is left out rather than shipped wrong.
