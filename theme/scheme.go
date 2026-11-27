package theme

// Scheme names a set of colors that every package of cli-kit with colors
// of its own has a version of, such as the notices of package notice and
// the widgets of package tui, so one name, from a flag or a configuration
// file, gives a tool one look throughout.
type Scheme string

// The schemes.
const (
	// SchemeDefault is the look of mittwald's tools: mittwald blue badges
	// on dark grey bars, salmon and purple accents.
	SchemeDefault Scheme = "default"
	// SchemeMuted is softer colors than SchemeDefault.
	SchemeMuted Scheme = "muted"
	// SchemeDeep is dark, saturated backgrounds with bright values.
	SchemeDeep Scheme = "deep"
	// SchemePastel is light backgrounds with dark ink.
	SchemePastel Scheme = "pastel"
)

// Schemes lists every scheme, SchemeDefault first.
var Schemes = []Scheme{SchemeDefault, SchemeMuted, SchemeDeep, SchemePastel}

// ParseScheme returns the scheme of name, and false where no scheme has
// that name.
func ParseScheme(name string) (Scheme, bool) {
	for _, s := range Schemes {
		if string(s) == name {
			return s, true
		}
	}
	return "", false
}
