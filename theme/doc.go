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
package theme
