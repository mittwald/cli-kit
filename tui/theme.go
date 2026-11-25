package tui

import "github.com/mittwald/cli-kit/theme"

// Theme is the set of styles the widgets draw with. Every field is a
// theme.Style; an empty one draws in the terminal's own colors.
type Theme struct {
	// Text is ordinary text.
	Text theme.Style
	// Muted is text that recedes: hints, placeholders, empty states.
	Muted theme.Style
	// Title is the title of a panel or dialog.
	Title theme.Style
	// Border is the border of a panel; FocusBorder of the panel holding
	// the focus.
	Border      theme.Style
	FocusBorder theme.Style
	// Header is the header row of a table.
	Header theme.Style
	// Cursor is the row under the cursor of a focused list; Cursor
	// without focus, CursorBlurred.
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
	// Info, Success, Warning and Error color messages by severity.
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

// ThemeFrom derives the styles of the widgets from a theme of package
// theme, so a tool that installed its own with theme.Use gets matching
// widgets. Backgrounds, which package theme has no roles for, use the
// reverse video of the terminal, so they read on dark and light
// backgrounds alike.
func ThemeFrom(t theme.Theme) Theme {
	return Theme{
		Muted:         t.Muted,
		Title:         t.Title,
		Border:        t.Border,
		FocusBorder:   t.Selected,
		Header:        t.Header,
		Cursor:        theme.Reverse,
		CursorBlurred: theme.Join(theme.Underline),
		Marked:        t.Accent,
		Key:           theme.Join(theme.Bold, t.Action),
		KeyHelp:       t.Muted,
		Accent:        t.Selected,
		Match:         theme.Join(theme.Underline, t.Accent),
		Field:         theme.Underline,
		FieldFocused:  theme.Join(theme.Underline, theme.Bold),
		On:            theme.Join(theme.Bold, t.Success),
		Off:           t.Muted,
		Info:          t.Info,
		Success:       t.Success,
		Warning:       t.Warning,
		Error:         t.Error,
		Scrollbar:     t.Border,
		ScrollThumb:   t.Qualifier,
		StatusBar:     t.Status,
	}
}

// DefaultTheme is ThemeFrom of the active theme of package theme.
func DefaultTheme() Theme { return ThemeFrom(theme.Active()) }
