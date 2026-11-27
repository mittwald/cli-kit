package tui

import (
	"strconv"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// Theme is the set of styles the widgets draw with. Every field is a
// theme.Style; an empty one draws in the terminal's own colors.
//
// The built-in themes come by scheme, as the colors of package notice do:
// [Schemes] holds a dark and a light version of each, and [ThemeOf] picks
// the one for the terminal's background. Surfaces are backgrounds rather
// than outlines: a title bar carries the title of a panel as a badge, a
// dialog and the row under the cursor are filled.
type Theme struct {
	// Text is ordinary text.
	Text theme.Style
	// Muted is text that recedes: hints, placeholders, empty states.
	Muted theme.Style
	// Title is a heading inside a widget, such as a group of the help.
	Title theme.Style
	// Border is a rule or a separator, and the border of a panel given
	// one; FocusBorder the border of the panel holding the focus.
	Border      theme.Style
	FocusBorder theme.Style
	// TitleBar is the line at the top of the screen.
	TitleBar theme.Style
	// Bar is the title bar of a panel or a dialog; Badge the title on it
	// while it has the focus, BadgeBlurred without.
	Bar          theme.Style
	Badge        theme.Style
	BadgeBlurred theme.Style
	// Header is the header row of a table.
	Header theme.Style
	// Cursor is the row under the cursor of a focused list; Cursor
	// without focus, CursorBlurred. Give Cursor a background rather than
	// reverse video: text drawn on it keeps its color, in a shade that
	// reads on that background.
	Cursor        theme.Style
	CursorBlurred theme.Style
	// Marked is a row marked for a bulk action, and the mark in front of
	// it.
	Marked theme.Style
	// Key is a key in the legend and the help, KeyHelp what it does.
	Key     theme.Style
	KeyHelp theme.Style
	// Accent marks something worth noticing, such as the active tab.
	Accent theme.Style
	// Match is the part of a row a filter matched.
	Match theme.Style
	// Field is a text field, FieldFocused one that has the focus.
	Field        theme.Style
	FieldFocused theme.Style
	// On and Off are the two states of a switch.
	On  theme.Style
	Off theme.Style
	// Info, Success, Warning and Error color messages by severity; a
	// notice in the status bar fills the bar with its color.
	Info    theme.Style
	Success theme.Style
	Warning theme.Style
	Error   theme.Style
	// Scrollbar is the track of a scroll bar, ScrollThumb its thumb.
	Scrollbar   theme.Style
	ScrollThumb theme.Style
	// StatusBar is the line at the bottom of the screen.
	StatusBar theme.Style
	// Dialog is the background of a dialog or a menu.
	Dialog theme.Style
}

// Scheme is a theme for terminals with a dark background and one for a
// light background.
type Scheme struct {
	Dark  Theme
	Light Theme
}

// Schemes are the built-in themes by the scheme they belong to, so one
// theme.Scheme picks the widgets' colors along with those of the other
// packages.
var Schemes = map[theme.Scheme]Scheme{
	theme.SchemeDefault: {
		Dark: build(palette{
			text: "#D0D0D8", muted: "#8C8AA0", border: "#4E4E62",
			titleBar: "#16161E", bar: "#262633", dialog: "#1E1E28", status: "#1E1E28", cursor: "#3A3A52",
			badge: "#4B38E8", badgeText: "#FFFFFF", badgeBlurred: "#3A3A52",
			accent: "#E7A086", key: "#9C94FF",
			info: "#5FAFD7", success: "#5ECC96", warning: "#F0BE64", error: "#F06276",
		}),
		Light: build(palette{
			text: "#22262E", muted: "#6A7080", border: "#C4C8D0",
			titleBar: "#DCDFE6", bar: "#E6E8EE", dialog: "#F2F3F6", status: "#E6E8EE", cursor: "#D4D0F6",
			badge: "#4B38E8", badgeText: "#FFFFFF", badgeBlurred: "#C4C8D0",
			accent: "#B0502E", key: "#4B38E8",
			info: "#1F5F99", success: "#2E7D32", warning: "#A66B00", error: "#C0392B",
		}),
	},
	theme.SchemeMuted: {
		Dark: build(palette{
			text: "#E4E7EC", muted: "#7A8290", border: "#4A5160",
			titleBar: "#1A1D23", bar: "#2C3038", dialog: "#23272E", status: "#23272E", cursor: "#3A404C",
			badge: "#4C8DB8", badgeText: "#F2F8FC", badgeBlurred: "#4A5160",
			accent: "#A99BE0", key: "#7FC4EA",
			info: "#7FB2D6", success: "#6CBF83", warning: "#E6B85C", error: "#E07A7A",
		}),
		Light: build(palette{
			text: "#333333", muted: "#8A9099", border: "#C9CDD3",
			titleBar: "#DFE3E8", bar: "#E9EBEF", dialog: "#F4F5F7", status: "#E9EBEF", cursor: "#D6DEE8",
			badge: "#3B6E91", badgeText: "#FFFFFF", badgeBlurred: "#C9CDD3",
			accent: "#5B4BA8", key: "#2A6496",
			info: "#3B6E91", success: "#2F7A45", warning: "#9A6E14", error: "#A33A3A",
		}),
	},
	theme.SchemeDeep: {
		Dark: build(palette{
			text: "#DCEAF4", muted: "#6B7484", border: "#2E333C",
			titleBar: "#0C1A26", bar: "#102434", dialog: "#0F1C28", status: "#102434", cursor: "#1D3B53",
			badge: "#8FD0F2", badgeText: "#102434", badgeBlurred: "#2E333C",
			accent: "#F2BD55", key: "#8FD0F2",
			info: "#8FD0F2", success: "#77C26E", warning: "#F2BD55", error: "#E06C6C",
		}),
		Light: build(palette{
			text: "#202020", muted: "#6B7484", border: "#B8C2CE",
			titleBar: "#C9D6E2", bar: "#D3DEE8", dialog: "#E6EDF3", status: "#D3DEE8", cursor: "#B9CCDD",
			badge: "#1D3B53", badgeText: "#FFFFFF", badgeBlurred: "#B8C2CE",
			accent: "#7A4A00", key: "#1D4F7A",
			info: "#1D4F7A", success: "#1C4A29", warning: "#5E4512", error: "#6E1C1C",
		}),
	},
	theme.SchemePastel: {
		Dark: build(palette{
			text: "#E0E0E0", muted: "#8A8F98", border: "#4A4E58",
			titleBar: "#2A2D34", bar: "#3A3E46", dialog: "#30333A", status: "#30333A", cursor: "#4A4F5A",
			badge: "#B4D8F0", badgeText: "#0D2638", badgeBlurred: "#5A5F6A",
			accent: "#F0B27A", key: "#BBA8F0",
			info: "#B4D8F0", success: "#B8E6C2", warning: "#F6D58E", error: "#F4B4B4",
		}),
		Light: build(palette{
			text: "#22262E", muted: "#6A7080", border: "#D3D8E0",
			titleBar: "#E3F0F9", bar: "#ECEFF3", dialog: "#F7F8FA", status: "#ECEFF3", cursor: "#DCE6F5",
			badge: "#B4D8F0", badgeText: "#0D2638", badgeBlurred: "#D3D8E0",
			accent: "#8A2A00", key: "#5B2A9A",
			info: "#1F6FA8", success: "#2E7D3E", warning: "#9A6A00", error: "#B03030",
		}),
	},
}

// PlainTheme draws with attributes only, for a terminal that takes no
// colors: reverse video marks the cursor and the badges, bold the titles
// and the keys. An application without a theme of its own draws in it
// where NO_COLOR or the color mode turns colors off.
var PlainTheme = Theme{
	Title:         theme.Bold,
	Badge:         theme.Join(theme.Bold, theme.Reverse),
	BadgeBlurred:  theme.Bold,
	Header:        theme.Bold,
	Cursor:        theme.Reverse,
	CursorBlurred: theme.Underline,
	Marked:        theme.Bold,
	Key:           theme.Bold,
	Accent:        theme.Bold,
	Match:         theme.Underline,
	Field:         theme.Underline,
	FieldFocused:  theme.Join(theme.Underline, theme.Bold),
	On:            theme.Bold,
	Info:          theme.Bold,
	Success:       theme.Bold,
	Warning:       theme.Bold,
	Error:         theme.Bold,
}

// ThemeOf returns the theme of scheme for the terminal's background, light
// where term.LightBackground says so and dark otherwise. An unknown scheme
// is theme.SchemeDefault.
func ThemeOf(scheme theme.Scheme) Theme {
	s, ok := Schemes[scheme]
	if !ok {
		s = Schemes[theme.SchemeDefault]
	}
	if term.LightBackground() {
		return s.Light
	}
	return s.Dark
}

// DefaultTheme is ThemeOf theme.SchemeDefault.
func DefaultTheme() Theme { return ThemeOf(theme.SchemeDefault) }

// palette is the colors a built-in theme is built from, each "#rrggbb".
type palette struct {
	text, muted, border                         string
	titleBar, bar, dialog, status, cursor       string
	badge, badgeText, badgeBlurred, accent, key string
	info, success, warning, error               string
}

func build(p palette) Theme {
	fg := func(hex string) theme.Style { return hexColor("38", hex) }
	bg := func(hex string) theme.Style { return hexColor("48", hex) }
	return Theme{
		Text:          fg(p.text),
		Muted:         fg(p.muted),
		Title:         theme.Join(theme.Bold, fg(p.text)),
		Border:        fg(p.border),
		FocusBorder:   fg(p.accent),
		TitleBar:      theme.Join(bg(p.titleBar), fg(p.text)),
		Bar:           theme.Join(bg(p.bar), fg(p.text)),
		Badge:         theme.Join(theme.Bold, bg(p.badge), fg(p.badgeText)),
		BadgeBlurred:  theme.Join(theme.Bold, bg(p.badgeBlurred), fg(p.text)),
		Header:        theme.Join(theme.Bold, fg(p.text)),
		Cursor:        theme.Join(bg(p.cursor), fg(p.text)),
		CursorBlurred: theme.Underline,
		Marked:        fg(p.accent),
		Key:           theme.Join(theme.Bold, fg(p.key)),
		KeyHelp:       fg(p.muted),
		Accent:        fg(p.accent),
		Match:         theme.Join(theme.Underline, fg(p.accent)),
		Field:         theme.Underline,
		FieldFocused:  theme.Join(theme.Underline, theme.Bold),
		On:            theme.Join(theme.Bold, fg(p.success)),
		Off:           fg(p.muted),
		Info:          fg(p.info),
		Success:       fg(p.success),
		Warning:       fg(p.warning),
		Error:         fg(p.error),
		Scrollbar:     fg(p.border),
		ScrollThumb:   fg(p.muted),
		StatusBar:     theme.Join(bg(p.status), fg(p.text)),
		Dialog:        theme.Join(bg(p.dialog), fg(p.text)),
	}
}

// hexColor is the SGR of "#rrggbb" as a 24-bit color, the foreground with
// layer 38 and the background with 48. The canvas turns it into the
// nearest of the 256 colors where the terminal takes no 24-bit colors.
func hexColor(layer, hex string) theme.Style {
	v, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil || len(hex) != 7 || hex[0] != '#' {
		panic("tui: bad color " + hex)
	}
	return theme.Style(layer + ";2;" + strconv.FormatUint(v>>16, 10) + ";" + strconv.FormatUint(v>>8&0xff, 10) + ";" + strconv.FormatUint(v&0xff, 10))
}
