// Package table renders rows of cells as an aligned text table, a framed
// table, a markdown pipe table, or CSV/TSV records.
//
// A [Table] is built with [New] and options, filled with [Table.Append], and
// written with [Table.Render]:
//
//	t := table.New(os.Stdout, []string{"Name", "Status"}, table.WithStyle(table.Rounded))
//	t.Append("api", "running")
//	t.Append("worker", "stopped")
//	err := t.Render()
//
// The default [Plain] style is a drop-in for text/tabwriter listings: columns
// padded to their widest cell and separated by two spaces, no trailing
// padding, no rules. The framed styles ([Light], [Rounded], [Heavy],
// [Double], [Fancy], [ASCII]) draw box rules around every cell. [Markdown]
// draws a GitHub-flavored pipe table that pastes into a document unchanged.
// [WithDelimiter] or [WithFormat] switch to CSV or TSV records.
//
// Widths are measured in terminal cells, not bytes: wide runes count two
// cells, and escape sequences a caller painted into a cell count none. A
// cell may span several lines; the whole row grows with it.
//
// Header cells and frame rules are painted with the active theme's Header and
// Border styles when term.Colored reports color for the writer, or as
// [WithColor] decides. Markdown and record output is never painted.
//
// # Text from elsewhere
//
// Markdown and record output is pasted, parsed or printed as it stands, and
// nothing in it is painted, so every cell becomes plain text there, whoever
// wrote it: escape sequences and control characters are removed as
// term.Plain removes them, a carriage return becomes a line break, and in a
// record a tab becomes a space. A record then carries no control character
// but the separators and line ends of its format.
//
// A drawn table shows its cells as they are by default, since it cannot tell
// the escape sequences a caller painted into a cell, a color or an OSC 8
// link, from ones that came with the data. Text from elsewhere goes through
// term.Sanitize or term.Plain before it is painted and appended, or the
// table does it for every cell: [SanitizeCells] keeps colors and removes
// everything else, [PlainCells] removes colors too.
package table
