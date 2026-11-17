// Package theme holds the colors the packages of this module paint with.
//
// A [Style] is a set of SGR parameters ("1;38;5;141"); a [Theme] assigns one
// to each role text plays on screen — a title, a muted note, the selected
// row, an error. Packages read their colors from the active theme, so a
// tool that wants its own palette calls [Use] once at start-up, usually
// with a copy of [Default] that changes a few roles. Whether anything is
// painted at all is decided by term.Colored, not by the theme;
// [Style.For] and [Theme.For] ask it for a given writer.
//
// [Theme.PaintDiff] paints a unified diff with the theme's diff roles, and
// [DiffScanner] tells its lines apart for code that shows only some of them.
// Both take the diff as it is and take nothing out of it: a diff that does
// not come from the program itself goes through term.Plain first.
// [Theme.PaintJSON] paints encoded JSON with the theme's JSON roles in the
// same way.
//
// [ParseStyle] reads a style a user wrote, such as "fg=white:bg=#aa0000:bold"
// in an environment variable, and [Style.PaintMatches] paints what a
// regular expression matches in text that may be painted already.
//
// [Hashed] gives a name a style of its own that stays the same across rows
// and runs, such as a color per author, picked from [NamePalette] or a
// palette of the caller's. [Theme.Hashed] does the same for output painted
// with a theme, and stays plain where the theme is [Plain].
package theme
