package table

import (
	"fmt"
	"strings"
)

// Line is one horizontal rule: a left cap, a fill repeated across every
// column, a junction between columns, and a right cap. A fill wider than one
// cell is cut where it does not fit a column a whole number of times. A Line
// without a fill is not drawn.
type Line struct {
	Left, Fill, Junction, Right string
}

// Enabled reports whether the rule is drawn.
func (l Line) Enabled() bool { return l.Fill != "" }

// Style is a complete table frame. The zero Style is [Plain]. Every fill and
// rule rune of the predefined styles is one cell wide, so a rule measures
// exactly as wide as the rows it separates.
type Style struct {
	// Name is how the style is selected by [LookupStyle].
	Name string
	// Top, HeaderRule, RowRule and Bottom are the horizontal rules above the
	// header, below it, between rows and below the last row.
	Top, HeaderRule, RowRule, Bottom Line
	// Left, Separator and Right are the vertical rules. A style without a
	// Separator is not framed: its columns are separated by Gutter instead.
	Left, Separator, Right string
	// Gutter separates the columns of a style without vertical rules. Empty
	// means two spaces.
	Gutter string
	// Flatten folds a multi-line cell onto one line, joining its lines with
	// LineBreak. A markdown row is one line by definition.
	Flatten bool
	// LineBreak joins the lines of a flattened cell. Empty means "<br>".
	LineBreak string
	// NoIndent drops the left indent [WithIndent] sets, for output that is
	// pasted verbatim and must start at column 0.
	NoIndent bool
	// EscapePipes escapes backslashes and pipes in cells, so a value that
	// carries a pipe cannot open a column of its own in a markdown table.
	EscapePipes bool
	// Verbatim marks output another document takes as it stands: it is never
	// painted, and escape sequences are removed from its cells.
	Verbatim bool
	// AlignMarkers marks right-aligned columns with a colon at the right end
	// of the header rule, the way markdown declares column alignment. There
	// the header rule is what makes the lines a table, so a style with
	// AlignMarkers draws it for a table without headers too, under an empty
	// header row.
	AlignMarkers bool
}

// Framed reports whether the style draws vertical rules.
func (s Style) Framed() bool { return s.Separator != "" }

// gutter is the column separator of an unframed style.
func (s Style) gutter() string {
	if s.Gutter == "" {
		return defaultGutter
	}
	return s.Gutter
}

// lineBreak joins the lines of a flattened cell.
func (s Style) lineBreak() string {
	if s.LineBreak == "" {
		return "<br>"
	}
	return s.LineBreak
}

// defaultGutter separates unframed columns, as text/tabwriter listings with
// a padding of two do.
const defaultGutter = "  "

// The predefined styles.
var (
	// Plain pads columns and separates them by two spaces, without rules.
	Plain = Style{Name: "plain"}
	// Light frames the table in light box-drawing rules.
	Light = Style{
		Name:       "light",
		Top:        Line{"┌", "─", "┬", "┐"},
		HeaderRule: Line{"├", "─", "┼", "┤"},
		RowRule:    Line{"├", "─", "┼", "┤"},
		Bottom:     Line{"└", "─", "┴", "┘"},
		Left:       "│", Separator: "│", Right: "│",
	}
	// Rounded is Light with rounded corners.
	Rounded = Style{
		Name:       "rounded",
		Top:        Line{"╭", "─", "┬", "╮"},
		HeaderRule: Line{"├", "─", "┼", "┤"},
		RowRule:    Line{"├", "─", "┼", "┤"},
		Bottom:     Line{"╰", "─", "┴", "╯"},
		Left:       "│", Separator: "│", Right: "│",
	}
	// Heavy frames the table in heavy box-drawing rules.
	Heavy = Style{
		Name:       "heavy",
		Top:        Line{"┏", "━", "┳", "┓"},
		HeaderRule: Line{"┣", "━", "╋", "┫"},
		RowRule:    Line{"┣", "━", "╋", "┫"},
		Bottom:     Line{"┗", "━", "┻", "┛"},
		Left:       "┃", Separator: "┃", Right: "┃",
	}
	// Double frames the table in double box-drawing rules.
	Double = Style{
		Name:       "double",
		Top:        Line{"╔", "═", "╦", "╗"},
		HeaderRule: Line{"╠", "═", "╬", "╣"},
		RowRule:    Line{"╠", "═", "╬", "╣"},
		Bottom:     Line{"╚", "═", "╩", "╝"},
		Left:       "║", Separator: "║", Right: "║",
	}
	// Fancy frames the table and its header in double rules but keeps single
	// verticals and between-row rules, so the header stands out while the
	// rows stay quiet.
	Fancy = Style{
		Name:       "fancy",
		Top:        Line{"╒", "═", "╤", "╕"},
		HeaderRule: Line{"╞", "═", "╪", "╡"},
		RowRule:    Line{"├", "─", "┼", "┤"},
		Bottom:     Line{"╘", "═", "╧", "╛"},
		Left:       "│", Separator: "│", Right: "│",
	}
	// ASCII frames the table in US-ASCII only, for terminals, pipes and logs
	// that mangle box-drawing runes.
	ASCII = Style{
		Name:       "ascii",
		Top:        Line{"+", "-", "+", "+"},
		HeaderRule: Line{"+", "-", "+", "+"},
		RowRule:    Line{"+", "-", "+", "+"},
		Bottom:     Line{"+", "-", "+", "+"},
		Left:       "|", Separator: "|", Right: "|",
	}
	// Markdown is a GitHub-flavored pipe table: a header rule and nothing
	// else, no indent, multi-line cells folded with <br>, pipes escaped, and
	// never painted. A table without headers gets an empty header row, since
	// a pipe table is one only with its header rule. It is what an --output
	// markdown flag prints; it is not
	// one of the [StyleNames], which list the drawn frames a --table-style
	// flag picks from.
	Markdown = Style{
		Name:       "markdown",
		HeaderRule: Line{"|", "-", "|", "|"},
		Left:       "|", Separator: "|", Right: "|",
		Flatten:      true,
		NoIndent:     true,
		EscapePipes:  true,
		Verbatim:     true,
		AlignMarkers: true,
	}
)

// drawnStyles are the styles [LookupStyle] resolves, in the order
// [StyleNames] lists them: plain first, because it is the default.
var drawnStyles = []*Style{&Plain, &Light, &Rounded, &Heavy, &Double, &Fancy, &ASCII}

// StyleNames returns the names of the drawn styles, plain first, for help
// text and shell completion of a --table-style flag.
func StyleNames() []string {
	names := make([]string, len(drawnStyles))
	for i, s := range drawnStyles {
		names[i] = s.Name
	}
	return names
}

// LookupStyle resolves a style name, ignoring case and surrounding space.
// It knows the drawn styles of [StyleNames], not [Markdown].
func LookupStyle(name string) (Style, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, s := range drawnStyles {
		if s.Name == name {
			return *s, true
		}
	}
	return Style{}, false
}

// ParseStyle is [LookupStyle] with an error that names the value and every
// accepted style, for flag validation.
func ParseStyle(name string) (Style, error) {
	if s, ok := LookupStyle(name); ok {
		return s, nil
	}
	return Style{}, fmt.Errorf("unknown table style %q: want one of %s", name, strings.Join(StyleNames(), ", "))
}

// escapeMarkdownCell escapes what markdown would read as structure inside a
// cell. Backslashes go first, so a backslash already in the data survives as
// literal text instead of turning the pipe behind it back into a separator.
func escapeMarkdownCell(cell string) string {
	if !strings.ContainsAny(cell, `\|`) {
		return cell
	}
	cell = strings.ReplaceAll(cell, `\`, `\\`)
	return strings.ReplaceAll(cell, "|", `\|`)
}
