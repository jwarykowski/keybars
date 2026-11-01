#!/usr/bin/env bash
# Writes THIRD_PARTY_NOTICES.md: the licence and notice files of every Go
# module linked into the release binaries, on each platform they're built for.
# CI runs it and fails if the file it writes differs from the committed one.
set -euo pipefail
# byte order, so every machine sorts modules the same way
export LC_ALL=C

cd "$(dirname "$0")/.."

product=keybars
platforms="linux darwin" # as .goreleaser.yaml builds
cgo=0
out=THIRD_PARTY_NOTICES.md
self=$(go list -m)

# licences of code a module bundles from elsewhere, beside its own
bundled() {
	case $1 in
	github.com/bearsh/hid) echo hidapi/LICENSE-bsd.txt ;;
	esac
}

modules() {
	for os in $platforms; do
		# -e: a missing embed (an unbuilt frontend) doesn't change what's linked
		GOOS=$os CGO_ENABLED=$cgo go list -e -deps \
			-f '{{with .Error}}error: {{.}}{{"\n"}}{{end}}{{with .Module}}{{.Path}}{{end}}' ./...
	done | sort -u | grep -vx "$self" | grep -v '^$'
}

list=$(modules)
if errors=$(grep '^error: ' <<<"$list" | grep -v 'no matching files found'); then
	echo "$errors" >&2
	exit 1
fi

{
	printf '# Third-party notices\n\n'
	printf '%s includes the following software, under these licences.\n' "$product"
	grep -v '^error: ' <<<"$list" | while read -r mod; do
		read -r version dir < <(go list -m -f '{{.Version}} {{.Dir}}' "$mod")
		printf '\n## %s %s\n' "$mod" "$version"
		{
			(cd "$dir" && find . -maxdepth 1 -type f \( -iname 'licen[cs]e*' -o -iname 'copying*' -o -iname 'notice*' \) ! -name '*.go' | sed 's|^\./||' | sort)
			bundled "$mod"
		} | while read -r file; do
			printf '\n### %s\n\n```\n' "$file"
			cat "$dir/$file"
			printf '\n```\n'
		done
	done

	# the standard library and runtime in every binary: Go's licence, the same
	# text as golang.org/x/sys's, taken from there so every machine writes it
	# alike (some Go packages leave it out)
	printf '\n## Go, its standard library and runtime\n\n### LICENSE\n\n```\n'
	cat "$(go list -m -f '{{.Dir}}' golang.org/x/sys)/LICENSE"
	printf '\n```\n'

	printf '\n## Keyboard maps from QMK Firmware\n\n'
	printf 'The maps in maps/qmk, and the ones keybars gen writes, are generated from\n'
	printf 'the keyboard definitions in QMK Firmware, https://github.com/qmk/qmk_firmware,\n'
	printf 'which is licensed under the GNU General Public License, version 2 or later.\n'
} >"$out"
