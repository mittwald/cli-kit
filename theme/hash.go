package theme

import (
	"hash/fnv"
	"strings"
)

// NamePalette is the palette [Hashed] picks from when it is given none:
// seven distinct mid-luminance hues of the 256-color palette — pink,
// turquoise, apricot, periwinkle, green, orchid and steel blue — that read on
// dark and light backgrounds and stay apart from the red and yellow of
// warnings and errors.
var NamePalette = []Style{
	Fg256(218),
	Fg256(116),
	Fg256(216),
	Fg256(105),
	Fg256(120),
	Fg256(177),
	Fg256(110),
}

// Hashed returns a style for name that stays the same: the same name gets
// the same style on every row, in every list and on every run, so a list can
// be scanned by who or what a row belongs to, such as the author of a
// commit. It picks from palette by the FNV-1a hash of the name, or from
// [NamePalette] when palette is empty.
//
// The name is compared without case and without the white space around it,
// so "Alice" and " alice" get one style. A blank name gets the zero Style,
// which paints nothing. Different names may share a style: a palette of
// seven tells seven names apart at best.
func Hashed(name string, palette []Style) Style {
	folded := strings.ToLower(strings.TrimSpace(name))
	if folded == "" {
		return ""
	}
	if len(palette) == 0 {
		palette = NamePalette
	}
	digest := fnv.New32a()
	// Writing to a hash never fails.
	_, _ = digest.Write([]byte(folded))
	return palette[uint64(digest.Sum32())%uint64(len(palette))]
}
