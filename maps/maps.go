// Package maps holds the built-in keyboard maps: <model>.txt in Keymapp's
// LED numbering for ZSA boards, and qmk/<id>.txt in the firmware's LED order,
// generated from QMK's keyboard definitions (scripts/maps.sh), for keyboards
// OpenRGB lights without giving their key matrix.
package maps

import (
	"embed"
	"encoding/json"
	"io/fs"
	"strings"
	"unicode"
)

// FS holds the built-in ZSA maps, one <model>.txt each.
//
//go:embed *.txt
var FS embed.FS

//go:embed qmk
var qmkFS embed.FS

// QMK holds the maps generated from QMK, one <id>.txt each.
var QMK = func() fs.FS {
	sub, err := fs.Sub(qmkFS, "qmk")
	if err != nil {
		panic(err)
	}
	return sub
}()

// Board is a keyboard with a map from QMK: its map's ID, its name, its path
// in QMK's keyboards, and the names a keyboard might report for it.
type Board struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	QMK   string   `json:"qmk"`
	Match []string `json:"match"`
}

// Boards lists the keyboards with a map from QMK.
func Boards() []Board {
	b, err := fs.ReadFile(QMK, "boards.json")
	if err != nil {
		panic(err)
	}
	var out []Board
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return out
}

// MatchQMK finds the map from QMK for a keyboard by the name it reports,
// ignoring case, spaces and punctuation. Names must match exactly, since a
// close name (a Q1 against a Q1 Pro) can be different hardware.
func MatchQMK(name string) (Board, bool) {
	n := normal(name)
	for _, b := range Boards() {
		for _, m := range append([]string{b.Name}, b.Match...) {
			if n != "" && normal(m) == n {
				return b, true
			}
		}
	}
	return Board{}, false
}

func normal(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}
