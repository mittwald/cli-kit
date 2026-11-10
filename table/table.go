package table

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/mittwald/cli-kit/format"
	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// cellPadding is the number of spaces a framed cell keeps on each side of its
// content, so text never touches a vertical rule.
const cellPadding = 1

// Align is how a column's cells sit within its width.
type Align int

const (
	// AlignLeft pads cells on the right. It is the default.
	AlignLeft Align = iota
	// AlignRight pads cells on the left, for numbers.
	AlignRight
)

// Table collects rows and renders them. Build one with [New].
type Table struct {
	w               io.Writer
	headers         []string
	rows            [][]string
	style           Style
	delimiter       rune
	indent          string
	rowGap          bool
	headerTransform func(string) string
	color           *bool
	theme           *theme.Theme
	align           []Align
	recordLineJoin  string
}

// Option configures a [Table]. Options apply in order, so a later option
// overrides an earlier one that sets the same thing.
type Option func(*Table)

// New returns a table that writes to w, with one column per header. A table
// without headers prints its rows alone.
func New(w io.Writer, headers []string, opts ...Option) *Table {
	t := &Table{
		w:              w,
		headers:        append([]string(nil), headers...),
		style:          Plain,
		recordLineJoin: "; ",
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// WithStyle draws the table in s. The default is [Plain].
func WithStyle(s Style) Option { return func(t *Table) { t.style = s } }

// WithFormat sets the table up for an output format: [format.Markdown]
// selects the [Markdown] style, [format.CSV] and [format.TSV] select record
// output, and every other format leaves the table as configured. Put it
// after [WithStyle], so a markdown request wins over a --table-style choice.
func WithFormat(f format.Format) Option {
	return func(t *Table) {
		switch f {
		case format.Markdown:
			t.style = Markdown
			t.delimiter = 0
		case format.CSV:
			t.delimiter = ','
		case format.TSV:
			t.delimiter = '\t'
		}
	}
}

// WithDelimiter writes records instead of drawing the table: a tab writes
// TSV, any other rune CSV with that field separator. Zero draws the table.
func WithDelimiter(r rune) Option { return func(t *Table) { t.delimiter = r } }

// WithIndent starts every drawn line with indent, such as " " to line a
// table up under a heading. Styles with NoIndent ignore it.
func WithIndent(indent string) Option { return func(t *Table) { t.indent = indent } }

// WithRowGap separates the rows of an unframed table with a blank line, which
// keeps multi-line rows apart. Framed styles separate rows with their RowRule
// instead, and record output ignores it.
func WithRowGap(on bool) Option { return func(t *Table) { t.rowGap = on } }

// WithHeaderTransform changes how header cells are drawn, such as
// strings.ToUpper. Record output keeps the headers as given to [New]: a
// spreadsheet column reads better as "Date" than as "DATE".
func WithHeaderTransform(fn func(string) string) Option {
	return func(t *Table) { t.headerTransform = fn }
}

// UppercaseHeaders draws header cells in upper case, the way kubectl-style
// listings do. Record output keeps the headers as given.
func UppercaseHeaders() Option { return WithHeaderTransform(strings.ToUpper) }

// WithColor decides whether header cells and rules are painted, instead of
// asking term.Colored for the writer. Verbatim styles and record output are
// never painted.
func WithColor(on bool) Option { return func(t *Table) { t.color = &on } }

// WithTheme paints header cells and rules with th's Header and Border
// styles instead of the active theme's.
func WithTheme(th theme.Theme) Option { return func(t *Table) { t.theme = &th } }

// WithAlignment sets the alignment of the columns, in order. Columns beyond
// the list are left-aligned.
func WithAlignment(aligns ...Align) Option {
	return func(t *Table) { t.align = append([]Align(nil), aligns...) }
}

// WithRecordLineJoin sets what joins the lines of a multi-line cell in CSV
// and TSV output, where a record is one line. The default is "; ".
func WithRecordLineJoin(sep string) Option { return func(t *Table) { t.recordLineJoin = sep } }

// Append adds a row. Cells may carry escape sequences and newlines; a row
// may be shorter or longer than the header.
func (t *Table) Append(cells ...string) {
	t.rows = append(t.rows, append([]string(nil), cells...))
}

// Len returns the number of rows appended so far.
func (t *Table) Len() int { return len(t.rows) }

// Render writes the table.
func (t *Table) Render() error {
	if t.delimiter != 0 {
		return t.renderRecords()
	}
	headers, rows := t.prepare()
	widths := columnWidths(headers, rows)
	if len(widths) == 0 {
		return nil
	}
	w := bufio.NewWriter(t.w)
	var err error
	if t.style.Framed() {
		err = t.renderFramed(w, headers, rows, widths)
	} else {
		err = t.renderPlain(w, headers, rows, widths)
	}
	if err != nil {
		return err
	}
	return w.Flush()
}

// TotalWidth returns the width of the drawn table in cells: the indent, every
// column, and the gutters or rules between them. Every line of a framed table
// is exactly this wide; an unframed table leaves its last column unpadded,
// so its lines may be shorter. It is useful to right-align something to the
// table's edge.
func (t *Table) TotalWidth() int {
	headers, rows := t.prepare()
	widths := columnWidths(headers, rows)
	total := term.VisibleWidth(t.lineIndent())
	for _, width := range widths {
		total += width
	}
	if len(widths) == 0 {
		return total
	}
	if !t.style.Framed() {
		return total + term.VisibleWidth(t.style.gutter())*(len(widths)-1)
	}
	total += 2 * cellPadding * len(widths)
	total += term.VisibleWidth(t.style.Left) + term.VisibleWidth(t.style.Right)
	return total + (len(widths)-1)*term.VisibleWidth(t.style.Separator)
}

func (t *Table) lineIndent() string {
	if t.style.NoIndent {
		return ""
	}
	return t.indent
}

func (t *Table) colored() bool {
	if t.style.Verbatim {
		return false
	}
	if t.color != nil {
		return *t.color
	}
	return term.Colored(t.w)
}

func (t *Table) palette() theme.Theme {
	if t.theme != nil {
		return *t.theme
	}
	return theme.Active()
}

func (t *Table) alignment(col int) Align {
	if col < len(t.align) {
		return t.align[col]
	}
	return AlignLeft
}

// prepare returns the cells as they are drawn: headers transformed, line
// endings normalized, and the style's escaping and folding applied. The
// table's own cells stay untouched, so rendering twice gives the same output.
func (t *Table) prepare() ([]string, [][]string) {
	cell := func(s string) string {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		if t.style.Verbatim {
			s = term.StripANSI(s)
		}
		if t.style.Flatten {
			s = strings.ReplaceAll(s, "\n", t.style.lineBreak())
		}
		if t.style.EscapePipes {
			s = escapeMarkdownCell(s)
		}
		return s
	}
	headers := make([]string, len(t.headers))
	for i, h := range t.headers {
		if t.headerTransform != nil {
			h = t.headerTransform(h)
		}
		headers[i] = cell(h)
	}
	rows := make([][]string, len(t.rows))
	for i, row := range t.rows {
		rows[i] = make([]string, len(row))
		for j, c := range row {
			rows[i][j] = cell(c)
		}
	}
	return headers, rows
}

func columnWidths(headers []string, rows [][]string) []int {
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = term.VisibleWidth(header)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], term.VisibleWidth(cell))
		}
	}
	return widths
}

// splitRow splits every cell of row into its lines and returns them with the
// row's height, the line count of its tallest cell.
func splitRow(row []string) ([][]string, int) {
	lines := make([][]string, len(row))
	height := 1
	for i, cell := range row {
		lines[i] = strings.Split(cell, "\n")
		height = max(height, len(lines[i]))
	}
	return lines, height
}

func lineAt(lines [][]string, col, line int) string {
	if col >= len(lines) || line >= len(lines[col]) {
		return ""
	}
	return lines[col][line]
}

func (t *Table) pad(cell string, width int, col int) string {
	if t.alignment(col) == AlignRight {
		return term.PadLeft(cell, width)
	}
	return term.PadRight(cell, width)
}

// renderPlain writes the unframed layout: columns padded and separated by the
// gutter, the last column on each line left unpadded, so no line carries
// trailing spaces.
func (t *Table) renderPlain(w io.Writer, headers []string, rows [][]string, widths []int) error {
	color := t.colored()
	header := t.palette().Header
	indent := t.lineIndent()
	gutter := t.style.gutter()
	writeRow := func(row []string, isHeader bool) error {
		lines, height := splitRow(row)
		for line := range height {
			last := -1
			for col := range widths {
				if lineAt(lines, col, line) != "" {
					last = col
				}
			}
			parts := make([]string, 0, last+1)
			for col := 0; col <= last; col++ {
				cell := lineAt(lines, col, line)
				if col < last || t.alignment(col) == AlignRight {
					cell = t.pad(cell, widths[col], col)
				}
				if isHeader {
					cell = header.PaintIf(color, cell)
				}
				parts = append(parts, cell)
			}
			if _, err := fmt.Fprintln(w, strings.TrimRight(indent+strings.Join(parts, gutter), " ")); err != nil {
				return err
			}
		}
		return nil
	}
	if len(headers) > 0 {
		if err := writeRow(headers, true); err != nil {
			return err
		}
	}
	for i, row := range rows {
		if i > 0 && t.rowGap {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if err := writeRow(row, false); err != nil {
			return err
		}
	}
	return nil
}

// renderFramed draws the table inside its style's frame: a top rule, the
// header, a header rule, and every row separated by the between-row rule. A
// row is one block whatever its height: a multi-line cell grows the whole
// row, and the last column is padded too, so the closing rule lands in the
// same column on every line.
func (t *Table) renderFramed(w io.Writer, headers []string, rows [][]string, widths []int) error {
	color := t.colored()
	th := t.palette()
	indent := t.lineIndent()
	border := func(s string) string { return th.Border.PaintIf(color, s) }
	rule := func(l Line) error {
		if !l.Enabled() {
			return nil
		}
		segments := make([]string, len(widths))
		for i, width := range widths {
			segment := strings.Repeat(l.Fill, width+2*cellPadding)
			if t.style.AlignMarkers && t.alignment(i) == AlignRight && width+2*cellPadding > 0 {
				segment = strings.Repeat(l.Fill, width+2*cellPadding-1) + ":"
			}
			segments[i] = segment
		}
		_, err := fmt.Fprintln(w, indent+border(l.Left+strings.Join(segments, l.Junction)+l.Right))
		return err
	}
	pad := strings.Repeat(" ", cellPadding)
	left, separator, right := border(t.style.Left), border(t.style.Separator), border(t.style.Right)
	writeRow := func(row []string, isHeader bool) error {
		lines, height := splitRow(row)
		for line := range height {
			parts := make([]string, len(widths))
			for col := range widths {
				cell := t.pad(lineAt(lines, col, line), widths[col], col)
				if isHeader {
					cell = th.Header.PaintIf(color, cell)
				}
				parts[col] = pad + cell + pad
			}
			if _, err := fmt.Fprintln(w, indent+left+strings.Join(parts, separator)+right); err != nil {
				return err
			}
		}
		return nil
	}
	if err := rule(t.style.Top); err != nil {
		return err
	}
	if len(headers) > 0 {
		if err := writeRow(headers, true); err != nil {
			return err
		}
		if err := rule(t.style.HeaderRule); err != nil {
			return err
		}
	}
	for i, row := range rows {
		if i > 0 {
			if err := rule(t.style.RowRule); err != nil {
				return err
			}
		}
		if err := writeRow(row, false); err != nil {
			return err
		}
	}
	return rule(t.style.Bottom)
}
