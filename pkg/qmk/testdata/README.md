Trimmed from QMK's keyboard definitions (https://github.com/qmk/qmk_firmware,
GPL-2.0-or-later): only each key's matrix position, x/y and size, each RGB
LED's matrix position, and whether the board is split, are kept.
`moonlander.json` and `voyager.json` come from `keyboards/zsa/*/keyboard.json`;
`planck.json`, `keychron_q1.json` and `kyria.json` from QMK's API
(https://keyboards.qmk.fm), which wraps the merged definition in
`{"keyboards": {...}}`. Used by the tests only; not built into keybars.
