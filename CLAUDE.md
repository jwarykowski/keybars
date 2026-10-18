# keybars

The free, MIT-licensed CLI: music as bars on a per-key RGB keyboard, through
cava, with ZSA boards over Keymapp's API and others through OpenRGB. Its
packages (`pkg/`, `maps/`) are also imported by keybars Pro, a separate paid
app. README.md has the detail; this file is what to keep in mind while
working here.

## Commands

- `go vet ./...`, `go test ./...`, with `GOTOOLCHAIN=auto`.
- After any dependency change: `scripts/notices.sh`. CI fails if
  THIRD_PARTY_NOTICES.md is stale (Dependabot PRs need this too).
- `scripts/maps.sh` regenerates the QMK maps and `maps/shapes.json` from
  QMK's API. keybars-www copies shapes.json with `make boards`.
- Keep `pkg/` free of anything Pro-only: nothing moves from this repo into
  Pro, and the CLI stays free.

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
- A map that can't be verified (ErgoDox EZ: thumbs not on their own matrix
  row) is left out rather than shipped wrong.
