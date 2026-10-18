#!/usr/bin/env bash
# Regenerates the maps keybars builds in from QMK's keyboard definitions:
# ZSA's Planck EZ in Keymapp's numbering (maps/*.txt), the
# boards in maps/qmk/boards.json in the firmware's LED order (maps/qmk/*.txt),
# and every board's shape for previews (maps/shapes.json). The Moonlander's
# and Voyager's maps were checked on real boards and are left as they are.
# The ErgoDox EZ is left out: its thumb keys don't sit on a matrix row of their
# own, so its Keymapp numbering can't be worked out without a real board.
# Needs network, go and jq.
#
# usage: scripts/maps.sh
set -euo pipefail

cd "$(dirname "$0")/.."
bin=$(mktemp -d)/keybars
trap 'rm -rf "$(dirname "$bin")"' EXIT
go build -o "$bin" .

# id, QMK path, and the layout ZSA ships
zsa=("planck zsa/planck_ez/glow LAYOUT_planck_1x2uC")
shapes=()

for z in "${zsa[@]}"; do
  read -r id path layout <<<"$z"
  "$bin" gen -layout "$layout" "$path" >"maps/$id.txt"
done
for z in "moonlander zsa/moonlander LAYOUT" "voyager zsa/voyager LAYOUT" "${zsa[@]}"; do
  read -r id path layout <<<"$z"
  shapes+=("$("$bin" gen -layout "$layout" -json "$path" | jq -c --arg id "$id" '{id: $id, order: "keymapp"} + .')")
done

while IFS=$'\t' read -r id name path; do
  "$bin" gen -order firmware "$path" >"maps/qmk/$id.txt"
  shapes+=("$("$bin" gen -order firmware -json "$path" | jq -c --arg id "$id" --arg name "$name" '{id: $id, order: "firmware"} + . + {name: $name}')")
done < <(jq -r '.[] | [.id, .name, .qmk] | @tsv' maps/qmk/boards.json)

printf '%s\n' "${shapes[@]}" | jq -s . >maps/shapes.json
echo "maps: ${#zsa[@]} ZSA, $(jq length maps/qmk/boards.json) from QMK, $(jq length maps/shapes.json) shapes"
