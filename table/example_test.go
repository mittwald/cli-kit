package table_test

import (
	"os"

	"github.com/mittwald/cli-kit/format"
	"github.com/mittwald/cli-kit/table"
)

func services(opts ...table.Option) *table.Table {
	t := table.New(os.Stdout, []string{"Name", "Status", "Replicas"},
		append([]table.Option{table.WithColor(false)}, opts...)...)
	t.Append("api", "running", "3")
	t.Append("worker", "stopped", "0")
	return t
}

func Example() {
	_ = services(table.UppercaseHeaders()).Render()
	// Output:
	// NAME    STATUS   REPLICAS
	// api     running  3
	// worker  stopped  0
}

func ExampleWithStyle() {
	_ = services(table.WithStyle(table.Rounded)).Render()
	// Output:
	// ╭────────┬─────────┬──────────╮
	// │ Name   │ Status  │ Replicas │
	// ├────────┼─────────┼──────────┤
	// │ api    │ running │ 3        │
	// ├────────┼─────────┼──────────┤
	// │ worker │ stopped │ 0        │
	// ╰────────┴─────────┴──────────╯
}

func ExampleWithFormat_markdown() {
	_ = services(table.WithFormat(format.Markdown), table.WithAlignment(table.AlignLeft, table.AlignLeft, table.AlignRight)).Render()
	// Output:
	// | Name   | Status  | Replicas |
	// |--------|---------|---------:|
	// | api    | running |        3 |
	// | worker | stopped |        0 |
}

func ExampleWithFormat_csv() {
	t := table.New(os.Stdout, []string{"Name", "Note"}, table.WithFormat(format.CSV))
	t.Append("api", "scaled up, then down")
	t.Append("worker", "line one\nline two")
	_ = t.Render()
	// Output:
	// Name,Note
	// api,"scaled up, then down"
	// worker,line one; line two
}

// Names and comments from a server are shown as plain text: the window title,
// the cleared screen and the colors they tried to set are gone.
func ExamplePlainCells() {
	t := table.New(os.Stdout, []string{"Name", "Comment"}, table.WithColor(false), table.PlainCells())
	t.Append("Ada\x1b]0;pwned\x07", "\x1b[2J\x1b[31mURGENT\x1b[0m: call me")
	_ = t.Render()
	// Output:
	// Name  Comment
	// Ada   URGENT: call me
}

// An export people open in a spreadsheet: a comment that starts like a
// formula is written as text. The number stays a number.
func ExampleEscapeFormulas() {
	t := table.New(os.Stdout, []string{"Name", "Balance", "Comment"}, table.WithFormat(format.CSV), table.EscapeFormulas())
	t.Append("Ada", "-12.50", "=HYPERLINK(\"https://evil.example\")")
	t.Append("Alan", "+3.00", "- none")
	_ = t.Render()
	// Output:
	// Name,Balance,Comment
	// Ada,-12.50,"'=HYPERLINK(""https://evil.example"")"
	// Alan,+3.00,'- none
}
