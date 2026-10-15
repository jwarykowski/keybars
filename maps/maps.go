// Package maps holds the built-in keyboard maps, <model>.txt, in Keymapp's
// LED numbering.
package maps

import "embed"

// FS holds the built-in keyboard maps, one <model>.txt each.
//
//go:embed *.txt
var FS embed.FS
