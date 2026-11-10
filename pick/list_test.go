package pick

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/mittwald/cli-kit/theme"
)

func TestFilterNarrowsAndKeepsTheSelection(t *testing.T) {
	l := testList(t, 5, []string{"1.4.24", "active"}, []string{"1.4.23", "inactive"}, []string{"1.5.0", "active"})

	l.apply(key{kind: keyDown})
	if got := l.selected(); got != 1 {
		t.Fatalf("selected() = %d, want 1", got)
	}
	// The row under the cursor still matches, so the cursor stays on it.
	typeText(l, "1.4")
	if got, want := l.matches, []int{0, 1}; !slices.Equal(got, want) {
		t.Fatalf("matches = %v, want %v", got, want)
	}
	if got := l.selected(); got != 1 {
		t.Fatalf("selected() = %d, want 1", got)
	}

	// Terms narrow instead of widening, and they may come from any column.
	typeText(l, " inactive")
	if got, want := l.matches, []int{1}; !slices.Equal(got, want) {
		t.Fatalf("matches = %v, want %v", got, want)
	}

	l.apply(key{kind: keyClearFilter})
	if got, want := l.matches, []int{0, 1, 2}; !slices.Equal(got, want) {
		t.Fatalf("matches after clearing = %v, want %v", got, want)
	}
}

// A filter matching nothing selects nothing, and the position line still says
// how much is hidden.
func TestNoMatchSelectsNothing(t *testing.T) {
	l := testList(t, 5, []string{"1.4.24"}, []string{"1.4.23"})
	typeText(l, "9.9")
	if got := l.selected(); got != -1 {
		t.Fatalf("selected() = %d, want -1", got)
	}
	if got := l.apply(key{kind: keyEnter}); got != actionNone {
		t.Fatalf("apply(Enter) = %v, want actionNone", got)
	}
	if got, want := l.position(), "0 of 2     · filter: 9.9"; got != want {
		t.Errorf("position() = %q, want %q", got, want)
	}
	if got := l.render(80); !slices.Contains(got, "  no match") {
		t.Errorf("render() = %q, want a no-match line", got)
	}
}

// Backspace deletes filter text and nothing else: on an empty filter it does
// nothing, unless the list allows it to leave.
func TestBackspaceOnAnEmptyFilter(t *testing.T) {
	l := testList(t, 5, []string{"a"}, []string{"b"})
	if got := l.apply(key{kind: keyBackspace}); got != actionNone {
		t.Errorf("Backspace on an empty filter = %v, want nothing", got)
	}
	typeText(l, "b")
	if got := l.apply(key{kind: keyBackspace}); got != actionNone || len(l.filter) != 0 {
		t.Errorf("Backspace = %v with filter %q, want the filter emptied", got, string(l.filter))
	}

	nested := newList(Options{Items: []Item{{Cells: []string{"a"}}}, DismissOnBackspace: true}, 0, false)
	if got := nested.apply(key{kind: keyBackspace}); got != actionBack {
		t.Errorf("Backspace with DismissOnBackspace = %v, want actionBack", got)
	}
}

// A list that takes what is typed offers the filter as a row of its own,
// after the rows it matches, and not where a row already carries that name,
// nor where the caller refuses it.
func TestTypedRowFollowsTheFilter(t *testing.T) {
	isVersion := func(text string) bool { return text[0] >= '0' && text[0] <= '9' }
	l := newList(Options{
		Items:     []Item{{Cells: []string{"1.4.25", "tagged"}}, {Cells: []string{"1.4.24"}}},
		Typed:     isVersion,
		TypedNote: "typed",
	}, 0, false)

	typeText(l, "1.4.2")
	if got, want := l.matches, []int{0, 1, l.typedIndex()}; !slices.Equal(got, want) {
		t.Fatalf("matches = %v, want both rows and then the typed one", got)
	}
	if got := l.selected(); got != 0 {
		t.Errorf("selected() = %d, want the cursor to stay on the first real row", got)
	}

	typeText(l, "6")
	if got, want := l.matches, []int{l.typedIndex()}; !slices.Equal(got, want) {
		t.Fatalf("matches = %v, want only the typed row", got)
	}
	if got := l.apply(key{kind: keyEnter}); got != actionSelect {
		t.Fatalf("apply(Enter) = %v, want the typed row chosen", got)
	}
	if got, want := l.render(80)[2], "❯ 1.4.26  typed"; got != want {
		t.Errorf("typed row = %q, want %q", got, want)
	}

	l.apply(key{kind: keyBackspace})
	typeText(l, "5")
	if got, want := l.matches, []int{0}; !slices.Equal(got, want) {
		t.Errorf("matches = %v, want no typed row for a name a row already has", got)
	}

	l.apply(key{kind: keyClearFilter})
	typeText(l, "tag")
	if got, want := l.matches, []int{0}; !slices.Equal(got, want) {
		t.Errorf("matches = %v, want no typed row for text the caller refuses", got)
	}
}

// Without Typed the filter is only ever a filter.
func TestUntypedListOffersNoTypedRow(t *testing.T) {
	l := testList(t, 5, []string{"1.4.24"})
	typeText(l, "9.9")
	if len(l.matches) != 0 {
		t.Errorf("matches = %v, want none", l.matches)
	}
}

func TestMovementStaysInsideTheList(t *testing.T) {
	l := numbered(t, 4, 10)

	l.apply(key{kind: keyUp})
	if l.cursor != 0 {
		t.Errorf("Up at the top moved to %d", l.cursor)
	}
	l.apply(key{kind: keyEnd})
	if l.cursor != 9 {
		t.Errorf("End = %d, want 9", l.cursor)
	}
	l.apply(key{kind: keyDown})
	if l.cursor != 9 {
		t.Errorf("Down at the bottom moved to %d", l.cursor)
	}
	l.apply(key{kind: keyPageUp})
	if want := 9 - l.visible; l.cursor != want {
		t.Errorf("PgUp = %d, want %d", l.cursor, want)
	}
	l.apply(key{kind: keyHome})
	if l.cursor != 0 || l.top != 0 {
		t.Errorf("Home left cursor %d, top %d", l.cursor, l.top)
	}
}

// The window follows the cursor, and the last page stops at the end of the
// list instead of scrolling past it.
func TestWindowFollowsTheCursor(t *testing.T) {
	l := numbered(t, 4, 10)

	l.apply(key{kind: keyPageDown})
	if got, want := l.cursor, 4; got != want {
		t.Fatalf("cursor = %d, want %d", got, want)
	}
	if got, want := l.top, 1; got != want {
		t.Errorf("top = %d, want %d", got, want)
	}
	l.apply(key{kind: keyEnd})
	if got, want := l.top, 6; got != want {
		t.Errorf("top at the end = %d, want %d", got, want)
	}
}

// An out-of-range Initial starts at the first row instead of crashing.
func TestInitialSelection(t *testing.T) {
	items := []Item{{Cells: []string{"a"}}, {Cells: []string{"b"}}, {Cells: []string{"c"}}}
	if got := newList(Options{Items: items, Initial: 2}, 0, false).selected(); got != 2 {
		t.Errorf("selected() = %d, want the preselected 2", got)
	}
	for _, initial := range []int{-1, 3, 99} {
		if got := newList(Options{Items: items, Initial: initial}, 0, false).selected(); got != 0 {
			t.Errorf("Initial %d: selected() = %d, want 0", initial, got)
		}
	}
}

// The block has to fit the terminal: a block taller than the screen would
// scroll, and the in-place redraw would then repaint the wrong lines.
func TestVisibleRowsFitTheTerminal(t *testing.T) {
	for _, testCase := range []struct {
		name                    string
		maxVisible, height, out int
		chrome                  int
	}{
		{name: "leaves room for the chrome and a spare line", height: 24, chrome: 5, out: 18},
		{name: "a note costs one row", height: 24, chrome: 6, out: 17},
		{name: "an unknown height falls back", height: 0, chrome: 5, out: defaultVisible},
		{name: "the cap wins when it is lower", maxVisible: 5, height: 24, chrome: 5, out: 5},
		{name: "the cap is ignored when it is higher", maxVisible: 50, height: 24, chrome: 5, out: 18},
		{name: "a tiny terminal still draws rows", height: 5, chrome: 5, out: minVisible},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := visibleRows(testCase.maxVisible, testCase.height, testCase.chrome); got != testCase.out {
				t.Errorf("visibleRows(%d, %d, %d) = %d, want %d",
					testCase.maxVisible, testCase.height, testCase.chrome, got, testCase.out)
			}
		})
	}
}

// A terminal that shrinks under an open list must not leave a block taller
// than the screen behind.
func TestResizeRefitsTheWindow(t *testing.T) {
	l := numbered(t, 0, 40)
	l.resize(0, 24)
	if got, want := l.visible, 18; got != want {
		t.Fatalf("visible = %d, want %d", got, want)
	}
	l.apply(key{kind: keyEnd})
	l.resize(0, 10)
	if got, want := l.visible, 4; got != want {
		t.Fatalf("visible after shrinking = %d, want %d", got, want)
	}
	if l.cursor < l.top || l.cursor >= l.top+l.visible {
		t.Errorf("cursor %d fell outside the window [%d, %d)", l.cursor, l.top, l.top+l.visible)
	}
}

const (
	testLongHint  = "↑/↓ move · PgUp/PgDn page · type to filter · Ctrl-U clear · Enter select · Esc cancel"
	testShortHint = "↑/↓ move · type to filter · Enter select · Esc cancel"
)

func TestRenderBlock(t *testing.T) {
	l := testList(t, 3, []string{"1.4.24", "active"}, []string{"1.4.23", ""},
		[]string{"1.4.21", "inactive"}, []string{"1.4.19", "inactive"})
	l.note = "Nothing else on this release line."

	lines := l.render(100)
	want := []string{
		"Which one?",
		"Nothing else on this release line.",
		"",
		"❯ 1.4.24  active",
		// Nothing follows the number on this row, so nothing pads it either.
		"  1.4.23",
		"  1.4.21  inactive",
		"",
		"1 of 4",
		testLongHint,
	}
	if !slices.Equal(lines, want) {
		t.Fatalf("render() =\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	l.apply(key{kind: keyEnd})
	if got, want := l.render(100)[5], "❯ 1.4.19  inactive"; got != want {
		t.Errorf("row = %q, want %q", got, want)
	}
}

// The key line steps down to a shorter complete one rather than being cut off
// mid-key, and names Esc "back" in a nested list.
func TestHintFitsTheWidth(t *testing.T) {
	l := testList(t, 3, []string{"a"})
	if got := l.hint(width(testLongHint)); got != testLongHint {
		t.Errorf("hint() = %q, want the long line", got)
	}
	if got := l.hint(width(testLongHint) - 1); got != testShortHint {
		t.Errorf("hint() = %q, want the short line", got)
	}
	if got := l.hint(20); width(got) > 20 {
		t.Errorf("hint() = %q, wider than the terminal", got)
	}
	l.nested = true
	if got := l.hint(200); !strings.HasSuffix(got, "Esc back") {
		t.Errorf("nested hint() = %q, want Esc named back", got)
	}
	l.keyLine = "Enter pick · Esc back"
	if got := l.hint(200); got != "Enter pick · Esc back" {
		t.Errorf("hint() with Options.Hint = %q, want the caller's line", got)
	}
}

// Every line is cut to the terminal's width: a wrapped row desynchronizes the
// redraw from the screen.
func TestRenderTruncatesToWidth(t *testing.T) {
	l := testList(t, 2, []string{"1.4.24", "a rather long note"})
	for _, line := range l.render(12) {
		if got := width(line); got > 12 {
			t.Errorf("line %q is %d cells wide, want at most 12", line, got)
		}
	}
}

// Wide runes count as two cells, so a row of them is cut to the width too.
func TestRenderRowsStayWithinWidthWithWideRunes(t *testing.T) {
	l := testList(t, 3, []string{"表示幅のテスト", "日本語の説明がここに入ります"}, []string{"😀😀😀", "emoji"})
	for _, n := range []int{10, 17, 25} {
		for _, line := range l.render(n) {
			if got := width(line); got > n {
				t.Errorf("width %d: line %q is %d cells wide", n, line, got)
			}
		}
	}
}

// A row names something and then qualifies it, and the cursor colors the row
// rather than lighting a background behind it.
func TestColoredRender(t *testing.T) {
	l := testList(t, 2, []string{"eu-1 (primary)"}, []string{"Legacy Backend (v1/archive)"})
	l.color = true
	l.title, l.subject = "Where should %s be installed?", "tool-1.2"

	lines := l.render(60)
	// The title names what is being decided inside a plain sentence.
	if got, want := lines[0], "Where should "+th.Subject.Paint("tool-1.2")+" be installed?"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}

	selected, other := lines[2], lines[3]
	if !strings.HasPrefix(selected, seq(th.Selected)+"❯") {
		t.Errorf("selected row = %q, want a colored marker", selected)
	}
	if strings.Contains(selected, "48;5;") {
		t.Errorf("selected row = %q, want no background", selected)
	}
	for _, want := range []string{seq(th.Selected) + "eu-1 ", seq(th.SelectedQualifier) + "(primary)"} {
		if !strings.Contains(selected, want) {
			t.Errorf("selected row = %q, want it to contain %q", selected, want)
		}
	}
	for _, want := range []string{seq(th.Subject) + "Legacy Backend ", seq(th.Qualifier) + "(v1/archive)"} {
		if !strings.Contains(other, want) {
			t.Errorf("row = %q, want it to contain %q", other, want)
		}
	}
	if strings.Contains(stripSGR(other), "❯") {
		t.Errorf("row below the cursor carries the marker: %q", other)
	}
	if got := width(selected); got >= 60 {
		t.Errorf("selected row covers %d cells, want only what it says", got)
	}
	for _, line := range lines[5:] {
		if !strings.HasPrefix(line, seq(th.Muted)) {
			t.Errorf("line %q is not muted", line)
		}
	}

	// A title without a subject is drawn in the title style as a whole.
	l.title, l.subject = "Pick a region", ""
	if got, want := l.render(60)[0], th.Title.Paint("Pick a region"); got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

// Whatever a row says in brackets is a qualifier, wherever it stands.
func TestParenSpans(t *testing.T) {
	for _, testCase := range []struct {
		cell string
		want []span
	}{
		{cell: "eu-1 (primary)", want: []span{{text: "eu-1 "}, {text: "(primary)", paren: true}}},
		{
			cell: "tool (1.4) on (linux) now",
			want: []span{
				{text: "tool "}, {text: "(1.4)", paren: true}, {text: " on "},
				{text: "(linux)", paren: true}, {text: " now"},
			},
		},
		{cell: "(all)", want: []span{{text: "(all)", paren: true}}},
		{cell: "eu-1 (primary)   ", want: []span{{text: "eu-1 "}, {text: "(primary)", paren: true}, {text: "   "}}},
		{cell: "a (b (c) d) e", want: []span{{text: "a "}, {text: "(b (c) d)", paren: true}, {text: " e"}}},
		{cell: "a (b", want: []span{{text: "a "}, {text: "(b", paren: true}}},
		{cell: "1.4.25+1-rc", want: []span{{text: "1.4.25+1-rc"}}},
		{cell: ""},
	} {
		if got := parenSpans(testCase.cell); !slices.Equal(got, testCase.want) {
			t.Errorf("parenSpans(%q) = %v, want %v", testCase.cell, got, testCase.want)
		}
	}
}

// Without color the same lines carry no escape sequence at all.
func TestPlainRenderCarriesNoEscapes(t *testing.T) {
	l := newList(Options{
		Items:      []Item{{Cells: []string{"1.4.24", "x"}, CellStyles: []theme.Style{"1"}, Detail: "full"}},
		CellStyles: []theme.Style{"", "38;5;1"},
		PaintCell:  func(int, string, bool) (string, bool) { return "\x1b[1mpainted\x1b[0m", true },
	}, 0, false)
	for _, line := range l.render(30) {
		if strings.Contains(line, "\x1b") {
			t.Errorf("line %q carries an escape sequence", line)
		}
	}
}

func TestDriveSelects(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		chunks [][]byte
		want   int
		err    error
	}{
		{name: "Enter picks the row under the cursor", chunks: [][]byte{[]byte("\x1b[B\r")}, want: 1},
		{name: "a filter then Enter", chunks: [][]byte{[]byte("21\r")}, want: 2},
		{name: "Esc goes back", chunks: [][]byte{{0x1b}}, want: -1, err: ErrBack},
		{name: "Ctrl-C interrupts", chunks: [][]byte{{0x03}}, want: -1, err: ErrInterrupted},
		{name: "Ctrl-D interrupts", chunks: [][]byte{{0x04}}, want: -1, err: ErrInterrupted},
		{name: "a closed input interrupts", chunks: nil, want: -1, err: ErrInterrupted},
		{
			name:   "a split arrow key is still an arrow key",
			chunks: [][]byte{{0x1b}, []byte("[B"), []byte("\r")},
			want:   1,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			l := testList(t, 5, []string{"1.4.24"}, []string{"1.4.23"}, []string{"1.4.21"})
			screen := &renderer{w: io.Discard}
			err := drive(l, screen, fixedSize(80, 24), 0, &scriptedInput{chunks: testCase.chunks})
			if !errors.Is(err, testCase.err) {
				t.Fatalf("drive() error = %v, want %v", err, testCase.err)
			}
			got := -1
			if err == nil {
				got = l.selected()
			}
			if got != testCase.want {
				t.Errorf("drive() selected %d, want %d", got, testCase.want)
			}
		})
	}
}

// A read failure that is not the end of input is reported as such.
func TestDriveReportsReadFailures(t *testing.T) {
	l := testList(t, 5, []string{"a"})
	failure := errors.New("device gone")
	err := drive(l, &renderer{w: io.Discard}, fixedSize(80, 24), 0, &scriptedInput{err: failure})
	if !errors.Is(err, failure) || errors.Is(err, ErrAborted) {
		t.Errorf("drive() error = %v, want the read failure", err)
	}
}

// Back and interrupt are both aborts, and neither is the other.
func TestErrorModel(t *testing.T) {
	for _, err := range []error{ErrBack, ErrInterrupted} {
		if !errors.Is(err, ErrAborted) {
			t.Errorf("%v does not match ErrAborted", err)
		}
	}
	if errors.Is(ErrBack, ErrInterrupted) || errors.Is(ErrInterrupted, ErrBack) {
		t.Error("ErrBack and ErrInterrupted match each other")
	}
}

// The action is drawn after every cell, where every row's starts, and in a
// color of its own; a row without one ends where its cells do.
func TestActionLane(t *testing.T) {
	l := newList(Options{
		Items: []Item{
			{Cells: []string{"7.0.7", ""}, Action: "install"},
			{Cells: []string{"7.0.6", "new version"}, Action: "merge #31"},
			{Cells: []string{"7.0.4", "installed ✓"}, Disabled: true},
		},
		MaxVisible: 5,
	}, 0, false)
	lines := l.render(80)
	for i, want := range []string{
		"❯ 7.0.7               install",
		"  7.0.6  new version  merge #31",
		"  7.0.4  installed ✓",
	} {
		if got := lines[2+i]; got != want {
			t.Errorf("row %d = %q, want %q", i, got, want)
		}
	}
	l.color = true
	if row := l.renderRow(0, 80); !strings.HasSuffix(row, th.SelectedAction.Paint("install")) {
		t.Errorf("renderRow() = %q, want the action in its own color", row)
	}
	if row := l.renderRow(1, 80); !strings.HasSuffix(row, th.Action.Paint("merge #31")) {
		t.Errorf("renderRow() = %q, want the action in its own color", row)
	}
	if row := l.renderRow(2, 80); !strings.Contains(row, th.Muted.Paint("7.0.4")) {
		t.Errorf("renderRow() = %q, want a disabled row receded, name and all", row)
	}
}

// A flag follows the name it is about, in the flag color on every row, and
// the columns after it make room for it.
func TestFlag(t *testing.T) {
	l := newList(Options{
		Items: []Item{
			{Cells: []string{"3.14.9", ""}, Flag: "×", Action: "create"},
			{Cells: []string{"3.14.8", "tagged ✓"}, Action: "recreate", Muted: true},
		},
		MaxVisible: 5,
	}, 0, false)
	lines := l.render(80)
	for i, want := range []string{
		"❯ 3.14.9 ×            create",
		"  3.14.8    tagged ✓  recreate",
	} {
		if got := lines[2+i]; got != want {
			t.Errorf("row %d = %q, want %q", i, got, want)
		}
	}
	l.color = true
	if row := l.renderRow(0, 80); !strings.Contains(row, th.Selected.Paint("3.14.9")+" "+th.Flag.Paint("×")) {
		t.Errorf("renderRow() = %q, want the flag after the name", row)
	}
}

// A disabled row cannot be chosen, and the position line says why Enter does
// nothing there. Typing matches the action too.
func TestDisabledRowCannotBePicked(t *testing.T) {
	l := newList(Options{
		Items: []Item{
			{Cells: []string{"1.4.25", "installed ✓"}, Disabled: true},
			{Cells: []string{"1.4.26"}, Action: "merge #31"},
		},
	}, 0, false)
	if got := l.apply(key{kind: keyEnter}); got != actionNone {
		t.Errorf("Enter on a disabled row = %v, want nothing", got)
	}
	if !strings.Contains(l.position(), "nothing to do") {
		t.Errorf("position() = %q, want it to say there is nothing to do", l.position())
	}
	typeText(l, "merge")
	if l.selected() != 1 {
		t.Errorf("filtering by action selected %d, want the merge", l.selected())
	}
	l.apply(key{kind: keyClearFilter})
	l.move(1)
	if got := l.apply(key{kind: keyEnter}); got != actionSelect {
		t.Errorf("Enter on an enabled row = %v, want it selected", got)
	}
}

// A muted row recedes, its action included, but can still be chosen; under
// the cursor it is drawn like any other row.
func TestMutedRowRecedesButCanBePicked(t *testing.T) {
	l := newList(Options{
		Items: []Item{
			{Cells: []string{"1.4.26"}, Action: "install"},
			{Cells: []string{"1.4.25"}, Action: "install", Muted: true},
		},
	}, 0, true)
	if row := l.renderRow(1, 80); !strings.Contains(row, th.Muted.Paint("1.4.25")) ||
		!strings.HasSuffix(row, th.Muted.Paint("install")) {
		t.Errorf("renderRow() = %q, want name and action receded", row)
	}
	l.move(1)
	if row := l.renderRow(1, 80); !strings.HasSuffix(row, th.SelectedAction.Paint("install")) {
		t.Errorf("renderRow() under the cursor = %q, want the action lit", row)
	}
	if got := l.apply(key{kind: keyEnter}); got != actionSelect {
		t.Errorf("Enter on a muted row = %v, want it selected", got)
	}
}
