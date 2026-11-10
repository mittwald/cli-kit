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
// [WithColor] decides. Markdown and record output is never painted, and
// escape sequences in their cells are removed.
package table
