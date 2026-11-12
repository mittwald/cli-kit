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
	cells           cellText
	escapeFormulas  bool
}

// cellText is what a drawn table does with the escape sequences and control
// characters in its cells: the zero cellText draws them as they are. See
// [SanitizeCells] and [PlainCells].
type cellText int

const (
	// cellsSanitized draws cells as term.SanitizeLines leaves them.
	cellsSanitized cellText = iota + 1
	// cellsPlain draws cells as term.Plain leaves them.
	cellsPlain
)

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
// strings.ToUpper. fn gets the text of a header cell, never the escape
// sequences a caller painted into it: a color or an OSC 8 link stays as it
// is, parameters and URL included, and fn is called for each piece of text
// between two of them. Record output keeps the headers as given to [New]: a
// spreadsheet column reads better as "Date" than as "DATE".
func WithHeaderTransform(fn func(string) string) Option {
	return func(t *Table) { t.headerTransform = fn }
}

// UppercaseHeaders draws header cells in upper case, the way kubectl-style
// listings do. Escape sequences in a header cell keep their case, and record
// output keeps the headers as given.
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

// EscapeFormulas makes CSV and TSV output safe to open in a spreadsheet when
// its cells hold text from elsewhere. A spreadsheet takes a cell that starts
// with "=", "+", "-" or "@" for a formula, which can read other cells, fetch
// a URL or start a program, and it skips a tab or a carriage return at the
// start to get there. A header or row cell that starts with one of these
// six, or with spaces and then one of the four, is written with a single
// quote in front of it, which a spreadsheet shows as text. A cell that is
// nothing but a number with a sign, such as "-12.5", is no formula and stays
// as it is.
//
// The quote is part of the field for every other reader of the records,
// which is why this is an option: use it for an export people open in a
// spreadsheet, not for one a program parses. Drawn tables ignore it.
func EscapeFormulas() Option { return func(t *Table) { t.escapeFormulas = true } }

// SanitizeCells makes the cells of a drawn table, its header cells
// included, safe to show when they hold text from elsewhere: each goes
// through term.Sanitize, which keeps colors, the data's as much as the
// caller's, and takes out every other escape sequence (an OSC 8 link the
// caller added, too) and every control character but the newline. Each line
// of a multi-line cell stands on its own, as term.SanitizeLines makes it,
// so a color never runs into the padding or rules beside it. Markdown and
// record output are always plain text and do not need it. See [PlainCells]
// for cells that should keep no colors at all.
func SanitizeCells() Option { return func(t *Table) { t.cells = cellsSanitized } }

// PlainCells makes the cells of a drawn table, its header cells included,
// plain text: each goes through term.Plain, which takes out every escape
// sequence, colors included, and every control character but the newline.
// Use it for a table of text from elsewhere that the program does not paint
// cell by cell; the header and rule colors the table paints itself stay.
// Markdown and record output are always plain text and do not need it.
func PlainCells() Option { return func(t *Table) { t.cells = cellsPlain } }

// Append adds a row. Cells may carry escape sequences and newlines; a row
// may be shorter or longer than the header: the missing cells are empty, in
// a drawn table and in records alike. A drawn table shows the cells as they
// are, but for tabs and carriage returns, unless [SanitizeCells] or
// [PlainCells] says otherwise, so text from elsewhere in a cell has to be
// made safe first, with one of these or term.Sanitize or term.Plain;
// Markdown and record output make every cell plain text.
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
// endings normalized, made safe as the style or the cell option asks, and the
// style's escaping and folding applied. Whichever way a cell is made, it
// holds neither a tab nor a carriage return afterwards, so its width is the
// same wherever on a line it is drawn. The table's own cells stay untouched,
// so rendering twice gives the same output.
func (t *Table) prepare() ([]string, [][]string) {
	cell := func(s string) string {
		switch {
		case t.style.Verbatim:
			s = plainCell(s)
		case t.cells == cellsPlain:
			s = term.Plain(strings.ReplaceAll(s, "\r\n", "\n"))
		case t.cells == cellsSanitized:
			s = sanitizedCell(strings.ReplaceAll(s, "\r\n", "\n"))
		default:
			s = shownCell(s)
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
			h = transformText(h, t.headerTransform)
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

// transformText returns s with fn applied to its text and its escape
// sequences left as they are, so a transform never rewrites the parameters
// of a color or the URL of a link: upper-casing "ESC [1m" would make it
// "ESC [1M", which deletes a line instead of setting bold.
func transformText(s string, fn func(string) string) string {
	if !strings.Contains(s, "\x1b") {
		return fn(s)
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if n := term.EscapeLen(s[i:]); n > 0 {
			b.WriteString(s[i : i+n])
			i += n
			continue
		}
		next := len(s)
		if j := strings.IndexByte(s[i:], 0x1b); j >= 0 {
			next = i + j
		}
		b.WriteString(fn(s[i:next]))
		i = next
	}
	return b.String()
}

// tabStop is how far apart the tab stops are that a tab in a cell advances
// to, as term counts them.
const tabStop = 8

// shownCell is a drawn cell that is shown as given: its escape sequences
// stay. Only what would move the cursor off the cell's grid goes. A tab
// becomes the spaces up to the next tab stop, counted from the start of the
// cell's line: the stop a terminal would move to depends on the column the
// cell is drawn at, so the same cell would be wider in one column than in
// another. A carriage return that does not end a line is dropped: it would
// draw the rest of the cell over the table to its left.
func shownCell(s string) string {
	if !strings.ContainsAny(s, "\t\r") {
		return s
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "")
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	column := 0
	for i := 0; i < len(s); {
		if n := term.EscapeLen(s[i:]); n > 0 {
			b.WriteString(s[i : i+n])
			i += n
			continue
		}
		next := len(s)
		if j := strings.IndexAny(s[i:], "\t\n\x1b"); j >= 0 {
			next = i + j
		}
		b.WriteString(s[i:next])
		column += term.WidthAt(s[i:next], column)
		i = next
		if i == len(s) {
			break
		}
		switch s[i] {
		case '\t':
			spaces := tabStop - column%tabStop
			b.WriteString(strings.Repeat(" ", spaces))
			column += spaces
			i++
		case '\n':
			b.WriteByte('\n')
			column = 0
			i++
		}
	}
	return b.String()
}

// plainCell makes a cell of verbatim output plain text, as term.Plain does,
// after turning every carriage return into a line break: such output is
// never painted, and a carriage return in a cell that is pasted or printed
// would draw the rest of its line over the start, where both halves should
// be read.
func plainCell(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return term.Plain(strings.ReplaceAll(s, "\r", "\n"))
}

// sanitizedCell is term.Sanitize for a drawn cell, every line standing on its
// own as term.SanitizeLines makes it, and a final newline kept as the empty
// last line a drawn cell gives it.
func sanitizedCell(s string) string {
	lines := term.SanitizeLines(s)
	if strings.HasSuffix(s, "\n") {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
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
			segment := fill(l.Fill, width+2*cellPadding)
			if t.style.AlignMarkers && t.alignment(i) == AlignRight {
				segment = fill(l.Fill, width+2*cellPadding-1) + ":"
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
	// Where the header rule declares the columns, as in markdown, it is what
	// makes the lines below a table: without headers it stands under an
	// empty header row.
	if len(headers) > 0 || (t.style.AlignMarkers && t.style.HeaderRule.Enabled()) {
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

// fill returns a rule segment of exactly width cells: pattern repeated, and
// for the cells left over the start of pattern, as much of it as fits, and
// then spaces, where a wide character had to be left out. The cut-off start
// is written without the escape sequences of pattern, so that none stays
// open. A pattern of one cell, which every predefined style has, is repeated
// width times.
func fill(pattern string, width int) string {
	cells := term.VisibleWidth(pattern)
	if cells <= 0 || width <= 0 {
		return strings.Repeat(" ", max(width, 0))
	}
	segment := strings.Repeat(pattern, width/cells)
	left := width % cells
	if left == 0 {
		return segment
	}
	start := ""
	for _, r := range term.StripANSI(pattern) {
		if term.VisibleWidth(start+string(r)) > left {
			break
		}
		start += string(r)
	}
	return segment + term.PadRight(start, left)
}
