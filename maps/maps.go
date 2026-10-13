// Package maps holds the built-in keyboard maps, <model>.txt, in Keymapp's
// LED numbering.
package maps

import "embed"

//go:embed *.txt
var FS embed.FS
