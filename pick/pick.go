package pick

import (
	"os"
	"slices"
	"strings"

	"github.com/mittwald/cli-kit/theme"
)

// Item is one selectable row. Cells are drawn as aligned columns, the first
// of them naming what the row is.
//
// Everything a row draws (Cells, Action, Flag, Detail and Details) is shown
// as plain single-line text: escape sequences and control characters in it
// are dropped, and a line break or a tab becomes a space. Rows made from
// what a server or a repository holds can therefore be handed in as they
// are. Colors come from CellStyles and Options.PaintCell, not from the text.
type Item struct {
	// Cells are the row's columns. The first names the row and carries the
	// weight; text in parentheses inside it recedes. Later cells qualify the
	// row and recede as a whole.
	Cells []string
	// Action is what choosing the row does, such as "install" or "merge #31".
	// It is drawn after every cell, in a lane and a color of its own, so the
	// thing a row is decided on sits in the same place on every row.
	Action string
	// Disabled marks a row there is nothing to do with: it is drawn receded
	// and Enter on it does nothing.
	Disabled bool
	// Muted recedes a row the way Disabled does, but it can still be chosen:
	// something that can be done, but that nobody is waiting on.
	Muted bool
	// Flag is a mark drawn right after the first cell in the theme's flag
	// color, on every row alike: something about the name worth stopping at.
	Flag string
	// Match overrides what type-to-filter searches. By default it searches
	// every cell and the action.
	Match string
	// MatchFields splits what the filter searches into parts, each of which
	// may demand a minimum term length: a field a short term must not reach,
	// such as an opaque identifier where any two characters hit some row, sets
	// MinTerm. It replaces Match when set.
	MatchFields []MatchField
	// Groups are the kinds of row this is, which views narrow the list by. A
	// row may be of several kinds and then shows in the view of each.
	Groups []string
	// Key identifies the row across views that hold their own rows: when the
	// view changes, the cursor stays on the row with the same key. Rows
	// without a key match nothing, and the cursor starts at the top.
	Key string
	// CellStyles overrides Options.CellStyles for this row, indexed like
	// Cells. An empty style falls back to the column's style.
	CellStyles []theme.Style
	// Detail is the row's full value, shown after the title while the row is
	// under the cursor. What a narrow column had to shorten stays readable
	// there.
	Detail string
	// Details is Detail in several parts, each in its own style, joined by
	// the separator the list uses elsewhere. It replaces Detail when set.
	Details []Field
}

// Field is one part of an item's Details.
type Field struct {
	Text  string
	Style theme.Style
}

// MatchField is one searchable part of a row. MinTerm is the shortest filter
// term that may search it; 0 lets every term through.
type MatchField struct {
	Text    string
	MinTerm int
}

func (i Item) filterText() string {
	if i.Match != "" {
		return i.Match
	}
	return strings.Join(append(slices.Clip(i.Cells), i.Action), " ")
}

// matchFields are the row's searchable parts, lowered once so the filter only
// compares on every keystroke.
func (i Item) matchFields() []MatchField {
	if len(i.MatchFields) == 0 {
		return []MatchField{{Text: strings.ToLower(i.filterText())}}
	}
	fields := make([]MatchField, 0, len(i.MatchFields))
	for _, field := range i.MatchFields {
		fields = append(fields, MatchField{Text: strings.ToLower(field.Text), MinTerm: field.MinTerm})
	}
	return fields
}

// View is one way of looking at a list. Tab and the right arrow switch to the
// next view, Shift-Tab and the left arrow to the previous one, and the filter
// carries over.
//
// A view either narrows Options.Items to the rows with a group among Groups
// (every row when Groups is empty), or holds rows of its own in Items or
// Load. When any view holds its own rows, all of them do: a view with neither
// Items nor Load is then empty, and row indexes refer to the items of the
// view the list ended on.
type View struct {
	// Name labels the view in the status line. An empty name is shown as the
	// view's position.
	Name string
	// Groups narrows Options.Items to the rows of these groups.
	Groups []string
	// Title replaces Options.Title while the view is shown.
	Title string
	// Items are the view's own rows.
	Items []Item
	// Load supplies the view's rows the first time it is shown, for rows that
	// are expensive to produce. The list shows Loading in their place, runs
	// Load once, and keeps what it returns. A failure is shown where the rows
	// would be, together with whatever rows Load did return.
	//
	// Load runs on a goroutine of its own while the list keeps taking keys:
	// Tab moves on to another view, and Esc or Ctrl-C leaves the list. Load
	// is not stopped by that. It runs to its end in the background, possibly
	// after the function that drew the list has returned, and what it returns
	// is then dropped; a Load that should stop early watches something the
	// caller cancels. It must not write to the terminal. A panic in Load is
	// raised again on the goroutine that drew the list. Where waiting for a
	// key cannot be interrupted, Load runs before the next key is read and
	// the list cannot be left until it returns: on Windows, and on macOS in
	// a process with so many files open that the terminal, or the pipe the
	// wait is interrupted through, has a descriptor past what select(2)
	// takes (FD_SETSIZE, 1024).
	Load func() ([]Item, error)
	// Loading is the line shown while Load runs; "" uses a default.
	Loading string
	// Empty is the line shown in place of rows when the view has none; ""
	// uses a default.
	Empty string
}

func (v View) holdsRows() bool { return v.Items != nil || v.Load != nil }

func (v View) shows(item Item) bool {
	if len(v.Groups) == 0 {
		return true
	}
	for _, group := range item.Groups {
		if slices.Contains(v.Groups, group) {
			return true
		}
	}
	return false
}

// Lens is how a list was narrowed: what was typed into its filter and the
// view it was left on. View holds that view's name, or, for a view without
// one, its position among the views, counted from 1, the way the status line
// labels it. An empty View names no view: a list opened with it starts on
// the view Options.View asks for.
type Lens struct {
	Filter string
	View   string
}

// Column priorities for Options.ColumnPriority. When a row does not fit the
// terminal, the lowest-ranked column is shortened first.
const (
	// PriorityLow suits a column whose full value is available elsewhere,
	// such as in the title line through Item.Detail.
	PriorityLow = iota
	// PriorityNormal is the priority of every column not ranked otherwise.
	PriorityNormal
	// PriorityHigh suits the column a reader actually scans.
	PriorityHigh
)

// Truncation is how a cell too wide for its column is shortened.
type Truncation int

const (
	// TruncateEnd cuts the end of the cell. It is the default.
	TruncateEnd Truncation = iota
	// TruncateMiddle cuts the middle of the cell, keeping both ends: right for
	// values told apart by their ends, such as long identifiers or paths.
	TruncateMiddle
)

// Options configures one list. Its texts (Title, Subject, Note, Hint,
// TypedNote and RangeUnit, and the names and lines of its Views) are shown
// as plain single-line text, as the rows are (see Item).
type Options struct {
	// Title is the question, drawn above the rows. It may hold one %s where
	// Subject goes.
	Title string
	// Subject is the thing the question is about, drawn in the theme's
	// subject style inside the title.
	Subject string
	// Note is an optional line under the title for what has to be said about
	// the offer as a whole.
	Note string
	// Items are the rows.
	Items []Item
	// Views are the ways of looking at the list that Tab switches between.
	// Views that narrow Items need at least two to be shown at all.
	Views []View
	// View is the index of the view the list opens on.
	View int
	// Initial is the index of the item the cursor starts on. An item the
	// filter or view hides, or an index out of range, starts at the first row.
	// Where the list opens on a view that loads its rows, the cursor goes
	// there once they have arrived.
	Initial int
	// MaxVisible caps how many rows are drawn at once; 0 derives the cap from
	// the terminal height.
	MaxVisible int
	// Hint replaces the key line under the list.
	Hint string
	// Nested says the list was opened from another prompt, so the key line
	// names Esc "back" instead of "cancel". Esc returns ErrBack either way.
	Nested bool
	// DismissOnBackspace lets Backspace on an empty filter leave the list the
	// way Esc does. It suits a nested prompt, where Backspace reads as back.
	DismissOnBackspace bool
	// Typed lets the list take an answer it does not hold: the filter is
	// offered as a row of its own, after the rows it matches, as long as
	// Typed accepts it and none of the rows shown has exactly that text as
	// its first cell. A row of that name which the view hides is not on
	// offer, so the text can be typed all the same. Only SelectOrType and
	// SelectManyOrType honor it.
	Typed func(string) bool
	// TypedNote is what the typed row says about itself, in its second
	// column.
	TypedNote string
	// Lens, when set, is the filter and view the list opens on, and is left
	// holding the ones the list was closed on, so the next list about the
	// same rows opens narrowed the same way.
	Lens *Lens
	// CellStyles colors the columns, indexed like Item.Cells. Columns without
	// a style are drawn the default way: the first bold, the rest receding.
	CellStyles []theme.Style
	// PaintCell paints inside a cell, for columns whose parts deserve colors
	// of their own. It gets the column, the cell text already cut to its
	// column, and whether the row is under the cursor, and returns the
	// painted text and true, or false to leave the cell to the default
	// styles. It is called only when color is on and the row is not receded.
	// What it returns is drawn as it is, so it adds colors to the text it
	// was given and nothing else.
	PaintCell func(column int, text string, selected bool) (string, bool)
	// DetailStyle is the style of Item.Detail in the title line; the theme's
	// info style when empty.
	DetailStyle theme.Style
	// ColumnPriority ranks the columns, indexed like Item.Cells, by how long
	// they keep their width when a row does not fit: the lowest-ranked column
	// is shortened first. Unranked columns are PriorityNormal.
	ColumnPriority []int
	// ColumnTruncation says how each column's cells are shortened, indexed
	// like Item.Cells. Unlisted columns are TruncateEnd.
	ColumnTruncation []Truncation
	// Range turns the list into a range selection: the first Enter anchors
	// the range on the row under the cursor, moving extends it, and the
	// second Enter ends the list. Esc drops an open range before it leaves
	// the list. Read the range back with SelectRange.
	//
	// A range covers every item between its two ends, so every one of them
	// is on screen while it is open: anchoring clears the filter and sets a
	// view that narrows the items aside, and Esc brings the view back.
	Range bool
	// RangeUnit names what the range covers, in the singular ("commit"), for
	// the summary in the title line; an "s" is appended for any count but
	// one.
	RangeUnit string

	// multi is set by SelectManyOrType: Space marks rows rather than going
	// into the filter.
	multi bool
}

// scoped reports whether the views hold their own rows.
func (o Options) scoped() bool {
	return slices.ContainsFunc(o.Views, View.holdsRows)
}

// totalItems counts the rows a list can show across every view. A view that
// loads its rows on demand may have some, so it counts as one.
func (o Options) totalItems() int {
	if !o.scoped() {
		return len(o.Items)
	}
	total := 0
	for _, view := range o.Views {
		total += len(view.Items)
		if view.Load != nil {
			total++
		}
	}
	return total
}

// Select draws the list on out, reads keys from in, and returns the index of
// the item chosen: an index into opts.Items, or, with views that hold their
// own rows, into the Items of the view the list ended on (see SelectView).
// in is switched to raw mode for the duration and restored before returning,
// and the drawn block is erased so the caller's output starts on a clean
// line. With Options.Range, Select returns the first row of the range.
func Select(in, out *os.File, opts Options) (int, error) {
	_, first, _, err := selectRows(in, out, opts)
	return first, err
}

// SelectRange is Select for a list with Options.Range: it returns the two
// ends of the chosen range in the order the rows are drawn, first above last.
// Without Options.Range both are the row chosen.
func SelectRange(in, out *os.File, opts Options) (first, last int, err error) {
	_, first, last, err = selectRows(in, out, opts)
	return first, last, err
}

// SelectView is Select for a list of views: it returns the index of the view
// the list ended on and the index of the item chosen. For views that hold
// their own rows the item index refers to that view's Items, otherwise to
// opts.Items.
func SelectView(in, out *os.File, opts Options) (view, index int, err error) {
	view, index, _, err = selectRows(in, out, opts)
	return view, index, err
}

func selectRows(in, out *os.File, opts Options) (int, int, int, error) {
	if opts.totalItems() == 0 {
		return 0, -1, -1, ErrNoItems
	}
	// An index is all these functions can report, and a typed answer is not
	// one.
	opts.Typed = nil
	state, err := runList(in, out, opts)
	if err != nil {
		return 0, -1, -1, err
	}
	first, last := state.selectedRange()
	return state.view, first, last, nil
}

// SelectOrType is Select for a list that also takes an answer it does not
// hold (see Options.Typed). It returns the index of the item chosen and "",
// or -1 and the text typed. A list without items is drawn all the same as
// long as it takes typed answers. It cannot be combined with views that hold
// their own rows.
func SelectOrType(in, out *os.File, opts Options) (int, string, error) {
	if opts.scoped() {
		return -1, "", errScopedChoice
	}
	if len(opts.Items) == 0 && opts.Typed == nil {
		return -1, "", ErrNoItems
	}
	opts.Range = false
	state, err := runList(in, out, opts)
	if err != nil {
		return -1, "", err
	}
	if index := state.selected(); index != state.typedIndex() {
		return index, "", nil
	}
	return -1, state.typedText(), nil
}

// SelectManyOrType is SelectOrType for a list that takes several rows at
// once: Space marks the row under the cursor or unmarks it, and Enter answers
// with every row marked. It returns the indexes of the items marked, in the
// order of opts.Items, and the texts typed and marked, in the order they were
// marked. With nothing marked, Enter answers with the row under the cursor.
// Space marks rather than filters, so the filter is a single term.
func SelectManyOrType(in, out *os.File, opts Options) ([]int, []string, error) {
	if opts.scoped() {
		return nil, nil, errScopedChoice
	}
	if len(opts.Items) == 0 && opts.Typed == nil {
		return nil, nil, ErrNoItems
	}
	opts.multi = true
	opts.Range = false
	state, err := runList(in, out, opts)
	if err != nil {
		return nil, nil, err
	}
	indexes, typed := state.chosen()
	return indexes, typed, nil
}

// runList draws the list and drives it until a row is chosen or the list is
// left.
func runList(in, out *os.File, opts Options) (*list, error) {
	session, err := openTerminal(in, out, true)
	if err != nil {
		return nil, err
	}
	defer session.close()

	_, height := session.size()
	state := newList(opts, height, session.color)
	state.theme = session.theme
	err = session.drive(state, opts.MaxVisible)
	// The list is left narrowed however it ended, since the next list asked
	// is about the same rows.
	if opts.Lens != nil {
		*opts.Lens = state.lens(*opts.Lens)
	}
	if err != nil {
		return nil, err
	}
	return state, nil
}
