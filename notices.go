package main

import _ "embed"

// notices is THIRD_PARTY_NOTICES.md, so every binary carries the licences of
// the code it links, however it was installed.
//
//go:embed THIRD_PARTY_NOTICES.md
var notices string
