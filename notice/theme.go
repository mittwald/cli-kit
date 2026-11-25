package notice

import (
	"maps"
	"regexp"
	"slices"

	"github.com/mittwald/cli-kit/theme"
)

// Level is how serious a notice is. It picks the label and the colors.
type Level int

// The levels, most serious first.
const (
	LevelError Level = iota
	LevelWarn
	LevelInfo
	LevelOK
	LevelDebug
)

// Levels lists every level, most serious first.
var Levels = []Level{LevelError, LevelWarn, LevelInfo, LevelOK, LevelDebug}

// String returns the label a notice of the level carries: ERROR, WARN,
// INFO, OK or DEBUG.
func (l Level) String() string {
	switch l {
	case LevelError:
		return "ERROR"
	case LevelWarn:
		return "WARN"
	case LevelInfo:
		return "INFO"
	case LevelOK:
		return "OK"
	case LevelDebug:
		return "DEBUG"
	}
	return "NOTICE"
}

// labelWidth is the width of the longest label, the width every badge is
// padded to.
const labelWidth = 5

// Layout is how a notice is laid out.
type Layout int

// The layouts.
const (
	// LayoutLine writes the label in its level's color, a colon and the
	// message: "WARN: …". A debug notice is a muted "+ …" line.
	LayoutLine Layout = iota
	// LayoutBadge writes the label as a badge, on its level's background,
	// padded to one width so the messages line up, and the message after it.
	LayoutBadge
	// LayoutBar is LayoutBadge with the message on a tint of the level's
	// color, filled to the right edge of the terminal.
	LayoutBar
	// LayoutBlock writes badge and message on the level's background, filled
	// to the right edge of the terminal; only the label is bold.
	LayoutBlock
)

// Padding is the space a badge has left and right of its label, in columns.
type Padding struct{ Left, Right int }

// Theme is everything about how a notice looks: the layout, the colors for
// dark and light terminals, the badge padding, whether the background
// reaches the edge of the terminal, and the patterns highlighted in a
// message.
//
// The built-in themes are [Line], [Badge], [Bar] and [Block], all in the
// [Default] colors. A Theme is a value: the With methods return a changed
// copy and leave the theme they are called on as it was, so a built-in
// theme cannot be changed by accident.
type Theme struct {
	// Name says what the theme is, for messages and tests.
	Name string
	// Layout is how a notice is laid out.
	Layout Layout
	// Colors are the colors for dark and light terminals.
	Colors Colors
	// Padding is the space around a badge's label. The built-in themes use
	// one column on each side; a Theme built from scratch has none unless it
	// sets one.
	Padding Padding
	// NoFill stops the background of LayoutBar and LayoutBlock at the end of
	// the text instead of filling the line to the right edge.
	NoFill bool
	// Patterns are highlighted in the message's own text, the text of the
	// format and of arguments that are not a [Value].
	Patterns []Pattern
}

// Pattern highlights what Match finds in a message in the colors of Role.
// Matches are searched in the message's own text only: never inside a
// [Value], whose role already colors it.
type Pattern struct {
	Match *regexp.Regexp
	Role  Role
}

// Colors are the colors of a theme: Dark for terminals with a dark
// background, Light for terminals with a light one. A color Light leaves
// empty is taken from Dark, so Light needs only what a light background
// changes.
type Colors struct {
	Dark  Palette
	Light Palette
}

// Palette is one set of colors.
type Palette struct {
	// Levels are the colors of each level.
	Levels map[Level]LevelColors
	// Values color a [Value] on the terminal's own background, as LayoutLine
	// and LayoutBadge show the message, and on the tint of LayoutBar.
	Values map[Role]theme.Style
	// Separator colors the colon after a LayoutLine label.
	Separator theme.Style
}

// LevelColors are the colors of one level.
type LevelColors struct {
	// Label colors the label of LayoutLine, and a debug line as a whole.
	Label theme.Style
	// Accent is the background of the badge, and of the whole line in
	// LayoutBlock.
	Accent theme.Style
	// Text colors text on Accent.
	Text theme.Style
	// Tint is the background of the message in LayoutBar.
	Tint theme.Style
	// TintText colors text on Tint.
	TintText theme.Style
	// Values color a [Value] on Accent, in LayoutBlock. Such a value is bold
	// as well.
	Values map[Role]theme.Style
}

// Line is LayoutLine in the [Default] colors.
var Line = Theme{Name: "line", Layout: LayoutLine, Colors: Default, Padding: Padding{1, 1}}

// Badge is LayoutBadge in the [Default] colors.
var Badge = Theme{Name: "badge", Layout: LayoutBadge, Colors: Default, Padding: Padding{1, 1}}

// Bar is LayoutBar in the [Default] colors.
var Bar = Theme{Name: "bar", Layout: LayoutBar, Colors: Default, Padding: Padding{1, 1}}

// Block is LayoutBlock in the [Default] colors.
var Block = Theme{Name: "block", Layout: LayoutBlock, Colors: Default, Padding: Padding{1, 1}}

// Themes lists the built-in themes.
var Themes = []Theme{Line, Badge, Bar, Block}

// WithColors returns t with other colors, such as [Muted], [Deep] or
// [Pastel].
func (t Theme) WithColors(c Colors) Theme {
	t.Colors = c
	return t
}

// WithPadding returns t with left and right columns of space around a
// badge's label.
func (t Theme) WithPadding(left, right int) Theme {
	t.Padding = Padding{Left: max(left, 0), Right: max(right, 0)}
	return t
}

// WithoutFill returns t with the background of LayoutBar and LayoutBlock
// ending with the text.
func (t Theme) WithoutFill() Theme {
	t.NoFill = true
	return t
}

// WithPatterns returns t with patterns added to the ones it has.
func (t Theme) WithPatterns(patterns ...Pattern) Theme {
	t.Patterns = append(slices.Clip(t.Patterns), patterns...)
	return t
}

// merge returns p with every color over sets, set over it.
func (p Palette) merge(over Palette) Palette {
	out := Palette{
		Levels:    make(map[Level]LevelColors, len(Levels)),
		Values:    maps.Clone(p.Values),
		Separator: pick(over.Separator, p.Separator),
	}
	if out.Values == nil {
		out.Values = make(map[Role]theme.Style)
	}
	for role, style := range over.Values {
		if style != "" {
			out.Values[role] = style
		}
	}
	for _, level := range Levels {
		out.Levels[level] = p.Levels[level].merge(over.Levels[level])
	}
	return out
}

func (c LevelColors) merge(over LevelColors) LevelColors {
	out := LevelColors{
		Label:    pick(over.Label, c.Label),
		Accent:   pick(over.Accent, c.Accent),
		Text:     pick(over.Text, c.Text),
		Tint:     pick(over.Tint, c.Tint),
		TintText: pick(over.TintText, c.TintText),
		Values:   maps.Clone(c.Values),
	}
	if out.Values == nil {
		out.Values = make(map[Role]theme.Style)
	}
	for role, style := range over.Values {
		if style != "" {
			out.Values[role] = style
		}
	}
	return out
}

// pick returns over unless it is the zero Style.
func pick(over, base theme.Style) theme.Style {
	if over != "" {
		return over
	}
	return base
}
