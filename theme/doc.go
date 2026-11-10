// Package theme holds the colors the packages of this module paint with.
//
// A [Style] is a set of SGR parameters ("1;38;5;141"); a [Theme] assigns one
// to each role text plays on screen — a title, a muted note, the selected
// row, an error. Packages read their colors from the active theme, so a
// tool that wants its own palette calls [Use] once at start-up. Whether
// anything is painted at all is decided by term.Colored, not by the theme.
package theme
