#!/usr/bin/env bash
# Runs keybars against a real OpenRGB SDK server with debug keyboards (ANSI,
# ISO and JIS, a Corne with VialRGB's wiring matrix, a Sofle with none) and
# checks which keys light.
#
#   scripts/openrgb-check.sh            # openrgb from PATH
#   OPENRGB=/path/to/openrgb scripts/openrgb-check.sh
#
# OpenRGB still scans for real devices at start; it only reads them.
set -euo pipefail

openrgb=${OPENRGB:-openrgb}
command -v "$openrgb" >/dev/null || { echo "no $openrgb: install OpenRGB or set OPENRGB" >&2; exit 1; }
port=${OPENRGB_PORT:-6799}
dir=$(mktemp -d)
trap 'kill "$pid" 2>/dev/null || true; rm -rf "$dir"' EXIT

cat >"$dir/OpenRGB.json" <<'JSON'
{
  "DebugDevices": {
    "devices": [
      {"type": "keyboard", "name": "Debug ANSI", "single": false, "linear": false, "keyboard": true, "layout": 1},
      {"type": "keyboard", "name": "Debug ISO", "single": false, "linear": false, "keyboard": true, "layout": 2},
      {"type": "keyboard", "name": "Debug JIS", "single": false, "linear": false, "keyboard": true, "layout": 5}
    ],
    "CustomDevices": [
      {"DeviceName": "Corne", "DeviceType": "keyboard", "DeviceDescription": "the matrix VialRGB sends: wiring rows, halves stacked",
       "DeviceLocation": "debug", "DeviceVersion": "1", "DeviceSerial": "debug",
       "DeviceZones": [{"name": "Keys", "type": "matrix", "leds_min": 54, "leds_max": 54, "leds_count": 54,
         "matrix_width": 6, "matrix_height": 8,
         "matrix_map": [[24, 23, 18, 17, 10, 9], [25, 22, 19, 16, 11, 8], [26, 21, 20, 15, 12, 7], [-1, -1, -1, 14, 13, 6],
                        [51, 50, 45, 44, 37, 36], [52, 49, 46, 43, 38, 35], [53, 48, 47, 42, 39, 34], [-1, -1, -1, 41, 40, 33]]}]},
      {"DeviceName": "Sofle", "DeviceType": "keyboard", "DeviceDescription": "no key matrix",
       "DeviceLocation": "debug", "DeviceVersion": "1", "DeviceSerial": "debug",
       "DeviceZones": [{"name": "Keys", "type": "linear", "leds_min": 72, "leds_max": 72, "leds_count": 72}]}
    ]
  }
}
JSON

"$openrgb" --config "$dir" --server --server-host 127.0.0.1 --server-port "$port" --noautoconnect >"$dir/openrgb.log" 2>&1 &
pid=$!
for _ in $(seq 60); do
  if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
    break
  fi
  kill -0 "$pid" 2>/dev/null || { cat "$dir/openrgb.log" >&2; exit 1; }
  sleep 0.5
done

"$openrgb" --version 2>/dev/null | head -1 || true
cd "$(dirname "$0")/.."
KEYBARS_OPENRGB="127.0.0.1:$port" GOTOOLCHAIN=auto go test -count=1 -run TestLiveOpenRGB -v . "$@"
