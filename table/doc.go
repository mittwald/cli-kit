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
// draws a GitHub-flavored pipe table that pastes into a document unchanged;
// without headers it has an empty header row, since a pipe table is one only
// with the rule below its header. [WithDelimiter] or [WithFormat] switch to
// CSV or TSV records.
//
// Widths are measured in terminal cells, not bytes: wide runes count two
// cells, and escape sequences a caller painted into a cell count none. They
// are the same in every locale: a character of ambiguous width, such as the
// rules of the frames, is one cell. A cell may span several lines; the whole
// row grows with it. A row may have fewer or more cells than the header; the
// missing ones are empty.
//
// A drawn cell holds neither a tab nor a carriage return, whatever was
// appended. A tab becomes the spaces up to the next tab stop, every 8
// columns counted from the start of the cell's line: the stop a terminal
// would move to depends on the column the cell is drawn at, which would make
// the same cell wider in one column than in another. A carriage return that
// does not end a line is dropped, since it would draw the rest of the cell
// over the table to its left. Every line of a table so keeps its width.
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
// A drawn table shows its cells as they are by default, tabs and carriage
// returns aside, since it cannot tell the escape sequences a caller painted
// into a cell, a color or an OSC 8 link, from ones that came with the data.
// Text from elsewhere goes through term.Sanitize or term.Plain before it is
// painted and appended, or the table does it for every cell: [SanitizeCells]
// keeps colors and removes everything else, [PlainCells] removes colors too.
//
// # Records
//
// CSV fields are quoted as RFC 4180 describes; TSV fields are never quoted.
// Records end in a line feed, not the CRLF of RFC 4180, as tools on a
// command line expect. Every record has as many fields as the longest row
// or the header, the missing ones empty. A record of one empty field is
// written as "" in CSV, because readers skip an empty line; TSV has no
// quotes, so there it is the empty line, which cut(1) and awk(1) count and
// a reader that skips empty lines does not.
//
// Record output is not protected against formula injection: a spreadsheet
// takes a cell that starts with "=", "+", "-" or "@" for a formula, and a
// cell with text from elsewhere may start with one. [EscapeFormulas] puts a
// single quote in front of such cells for exports people open in a
// spreadsheet; without it, cells are written as they are, which is what a
// program reading the records needs.
package table
