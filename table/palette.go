package table

import (
	"strconv"

	"github.com/mittwald/cli-kit/theme"
)

// Palette holds the colors a listing is painted with beyond the rules of
// the theme: its header cells, its columns, and the cells that show how
// long ago something happened.
type Palette struct {
	// Header paints the header cells.
	Header theme.Style
	// Columns paint the columns that have no colors of their own, in turn.
	Columns []theme.Style
	// Age paints a point in time shown as an age.
	Age theme.Style
	// FreshAge paints an age of less than an hour.
	FreshAge theme.Style
}

// Specifications of the default palette.
const (
	defaultHeader   = "248:bold"
	defaultAge      = "245"
	defaultFreshAge = "40:bold"
)

var defaultColumns = []string{"38", "42", "222", "104", "202", "207"}

// NewPalette returns the default palette with the changes the environment
// asks for, so a user can repaint the listings of a tool. getenv reads a
// variable, such as os.Getenv, and prefix starts the names of the
// variables: <prefix>_COLOR_TABLE_HEADER, <prefix>_COLOR_TABLE_COLUMN_1 to
// _6, <prefix>_TEXT_COLOR_DURATION and <prefix>_TEXT_DURATION_FRESH, each a
// style as theme.ParseStyle reads it, such as "160:bold". A variable that
// is empty or holds no style keeps the default. An empty prefix or a nil
// getenv gives the defaults.
func NewPalette(getenv func(string) string, prefix string) Palette {
	style := func(suffix, def string) theme.Style {
		if getenv != nil && prefix != "" {
			if s, ok := theme.ParseStyle(getenv(prefix + suffix)); ok {
				return s
			}
		}
		s, _ := theme.ParseStyle(def)
		return s
	}
	palette := Palette{
		Header:   style("_COLOR_TABLE_HEADER", defaultHeader),
		Age:      style("_TEXT_COLOR_DURATION", defaultAge),
		FreshAge: style("_TEXT_DURATION_FRESH", defaultFreshAge),
	}
	for i, def := range defaultColumns {
		palette.Columns = append(palette.Columns, style("_COLOR_TABLE_COLUMN_"+strconv.Itoa(i+1), def))
	}
	return palette
}
