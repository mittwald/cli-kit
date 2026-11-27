// Package tui builds full-screen terminal applications from widgets: lists
// and tables that filter as you type, text fields and editors, switches,
// radio buttons and check lists, dialogs and menus, panels, tabs and
// pages, a key legend and a help screen, and a status bar.
//
// Where package pick asks one question in the lines below the cursor, a tui
// application takes the whole screen until it quits, the way lazygit, k9s
// or htop do.
//
// # Applications
//
// An [App] shows a tree of widgets: [New] takes its root, [App.Run] takes
// the terminal over (raw mode, the alternate screen, bracketed paste, the
// mouse with [WithMouse]) and puts it back however the run ends, also on a
// panic or a signal. Every change is drawn into a grid of cells and only the
// cells that changed are written.
//
//	list := tui.NewList(func(t Ticket) []tui.Span { ... }).SetColumns(...)
//	list.OnSelect = open
//	root := tui.VBox(tui.Grow(tui.NewPanel("Tickets", list)), tui.Fit(tui.NewLegend()))
//	app := tui.New(root)
//	app.Keymap().Bind("quit", app.Quit, "q")
//	if err := app.Run(); errors.Is(err, tui.ErrNotATerminal) { ... }
//
// The application and its widgets belong to the goroutine that runs it.
// Work that blocks, such as a request to an API, runs through [App.Go],
// which hands its result back to that goroutine; [App.Busy] shows a spinner
// meanwhile and [App.Notify] a message afterwards.
//
// # Widgets
//
// A [Widget] draws itself into a [Canvas]. What else it does it says by the
// interfaces it implements: [Focusable] takes the focus and keys,
// [Container] holds other widgets, [Bindings] has keys of its own, [Paster]
// takes pasted text, [Clicker] takes the mouse and [Sizer] tells layouts its
// size. Any type can be a widget; the ones here are building blocks:
//
//   - Layout: [VBox], [HBox] (children sized [Grow], [GrowBy], [Fixed] or
//     [Fit]), [Panel], [Pages], [Tabs], [Pad], [Spacer], [Keyed]
//   - Text: [Label], [Viewport] (a scrolling, searchable pager), [Badge]
//     (on a [Solid] color), [GradientText]
//   - Motion: [Shimmer] (text whose colors flow while work runs),
//     [ProgressBar] (a bar with its label inside that glides to its
//     value); any widget that is [Animated] keeps the screen redrawing
//   - Input: [Input], [TextArea], [Toggle] (checkbox or switch),
//     [RadioGroup], [CheckList], [Select], [Button], [Form]
//   - Choosing: [List] (a list or a table, with a filter, marks and pages
//     loaded on demand)
//   - Layers: [Dialog], [App.Push], [App.Confirm], [App.Prompt],
//     [App.Choose], [App.Menu], [App.EditText]
//   - Chrome: [NewTitleBar], [Legend], [StatusBar], the help on "?"
//
// # Keys
//
// A key goes first to the widget with the focus, then up the tree to the
// widgets around it, and last to the application's [Keymap]. A widget that
// takes text, such as a field, or a list while its filter is open, gets
// characters before any binding, so typing "q" into a field does not quit.
// Tab and Shift-Tab move the focus, Esc closes the topmost layer, Ctrl-C
// quits and "?" or F1 shows every binding that applies.
//
// The widgets know the keys other terminal programs taught their users:
//
//	↑ ↓  k j            move                      vim, less, lazygit, k9s
//	g G  home end       first, last               vim, less
//	ctrl+u ctrl+d       half a page               vim, less
//	pgup pgdown         page                      everywhere
//	/   n N             filter or search, matches vim, less, k9s
//	enter               open, choose              everywhere
//	space               mark, toggle              lazygit, mutt, ranger
//	esc                 back, cancel, clear       everywhere
//	tab shift+tab       next, previous field      forms, lazygit panes
//	?   f1              help                      lazygit, k9s, htop
//	ctrl+a ctrl+e ...   line editing              readline, emacs
//
// The [Legend] shows the bindings of the focused widget first and those
// around it after, cut to the width of the screen; bindings marked Hidden
// show only in the help.
//
// # Text
//
// Text drawn with a [Canvas] is made safe there: control characters and
// escape sequences are left out, so text from a server cannot move the
// cursor or recolor the screen. Widgets that take text from elsewhere
// (labels, list cells, titles) also fold it onto one line or wrap it.
// Colors are styles of package theme, given with each [Span]. Text on a
// background, such as the row under the cursor, keeps its color in a shade
// that reads on that background.
//
// # Themes
//
// [Theme] holds the styles of the widgets. They draw with surfaces rather
// than outlines: a panel's title is a badge on a bar across its top, a
// dialog and the row under the cursor are filled, and a notice fills the
// status bar with its level's color. The built-in themes are in [Schemes],
// named by the theme.Scheme names package notice uses too, each with a
// version for dark and one for light terminals; [ThemeOf] picks one and
// [WithTheme] applies it. Without a theme of its own an application draws
// in [DefaultTheme], or in [PlainTheme] where the terminal takes no colors.
// 24-bit colors turn into the nearest of the 256 colors where the terminal
// does not announce 24-bit colors.
//
// # Tests
//
// [Headless] runs an application without a terminal: keys go in, the screen
// comes out as text. Applications built from the package can test their
// screens the same way the package tests its widgets.
//
// # Platforms
//
// On Unix, a resize is picked up at once (SIGWINCH), Ctrl-Z can hand the
// terminal to the shell ([App.Suspend]), and the read that waits for keys is
// ended when the application quits. On Windows the size is polled,
// Ctrl-Break and closing the console put the terminal back before the
// process ends with status 1, there is no suspend, and the read waits for
// one more key before it ends; that key is dropped.
package tui
