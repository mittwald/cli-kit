package pick

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func rangeItems() []Item {
	return []Item{
		{Cells: []string{"91ecb94", "newest change"}, Detail: "91ecb94"},
		{Cells: []string{"5d6e7f8", "middle change"}, Detail: "5d6e7f8"},
		{Cells: []string{"1a2b3c4", "oldest change"}, Detail: "1a2b3c4"},
	}
}

func driveRange(t *testing.T, opts Options, chunks ...string) (int, int, error) {
	t.Helper()
	l := newList(opts, 24, false)
	source := &scriptedInput{noGrace: true}
	for _, chunk := range chunks {
		source.chunks = append(source.chunks, []byte(chunk))
	}
	if err := drive(l, &renderer{w: io.Discard}, fixedSize(80, 24), 0, source); err != nil {
		return -1, -1, err
	}
	first, last := l.selectedRange()
	return first, last, nil
}

func TestSelectRangeAnchorsThenExtends(t *testing.T) {
	opts := Options{Title: "t", Items: rangeItems(), Range: true}
	if first, last, err := driveRange(t, opts, "\r", "\x1b[B", "\r"); err != nil || first != 0 || last != 1 {
		t.Fatalf("range = %d..%d, err %v; want the first two rows", first, last, err)
	}
	// Extending upward from a lower row yields the same order.
	if first, last, err := driveRange(t, opts, "\x1b[B\x1b[B", "\r", "\x1b[A", "\r"); err != nil || first != 1 || last != 2 {
		t.Fatalf("range = %d..%d, err %v; want rows 1 and 2", first, last, err)
	}
	// Enter twice is a range of one row.
	if first, last, err := driveRange(t, opts, "\r", "\r"); err != nil || first != 0 || last != 0 {
		t.Fatalf("range = %d..%d, err = %v; want row 0 twice", first, last, err)
	}
}

// Esc drops an open range before it leaves the list.
func TestSelectRangeEscapeDropsTheRangeFirst(t *testing.T) {
	opts := Options{Items: rangeItems(), Range: true}
	first, last, err := driveRange(t, opts, "\r", "\x1b[B", "\x1b", "\x1b[B", "\r", "\r")
	if err != nil || first != 2 || last != 2 {
		t.Fatalf("range = %d..%d, err %v; want the row the second range opened on", first, last, err)
	}
	if _, _, err := driveRange(t, opts, "\x1b"); !errors.Is(err, ErrBack) {
		t.Fatalf("err = %v, want ErrBack", err)
	}
}

// Without Range a single Enter ends the list, and both ends are that row.
func TestSelectRangeWithoutRangeMode(t *testing.T) {
	if first, last, err := driveRange(t, Options{Items: rangeItems()}, "\x1b[B", "\r"); err != nil || first != 1 || last != 1 {
		t.Fatalf("range = %d..%d, err = %v; want row 1 twice", first, last, err)
	}
}

// A filter may find the first end, but it cannot survive the range: a row
// hidden between the ends would be chosen without ever being shown.
func TestSelectRangeClearsTheFilterOnAnchor(t *testing.T) {
	l := newList(Options{Items: rangeItems(), Range: true}, 24, false)
	l.setFilter([]rune("oldest"))
	if len(l.matches) != 1 {
		t.Fatalf("matches = %d, want the filter to have narrowed the list", len(l.matches))
	}
	l.apply(key{kind: keyEnter})
	if len(l.filter) != 0 || len(l.matches) != 3 {
		t.Fatalf("filter = %q over %d matches, want it cleared", string(l.filter), len(l.matches))
	}
	if first, last := l.selectedRange(); first != 2 || last != 2 {
		t.Fatalf("range = %d..%d, want the row the filter had found", first, last)
	}
	// Typing does not reopen the filter while the range is open, and views
	// do not switch.
	l.apply(key{kind: keyRune, rune: 'x'})
	l.apply(key{kind: keyClearFilter})
	l.apply(key{kind: keyBackspace})
	l.apply(key{kind: keyTab})
	if len(l.filter) != 0 || len(l.matches) != 3 {
		t.Fatalf("filter = %q over %d matches, want the visible set frozen", string(l.filter), len(l.matches))
	}
}

// The whole span is lit, the marker stays on the cursor row, and the title
// says how much the range covers and where it runs.
func TestRenderShowsTheOpenSpan(t *testing.T) {
	l := newList(Options{Title: "Changes to compare:", Items: rangeItems(), Range: true, RangeUnit: "change"}, 24, true)
	if got := l.hint(200); !strings.Contains(got, "Enter opens the range") {
		t.Errorf("hint = %q, want the range key", got)
	}
	l.apply(key{kind: keyEnter})
	l.apply(key{kind: keyDown})
	if got := l.hint(200); !strings.Contains(got, "Enter selects the range") {
		t.Errorf("hint = %q, want the open-range keys", got)
	}
	lines := l.render(100)
	title := stripSGR(lines[0])
	if !strings.Contains(title, "2 changes") || !strings.Contains(title, "5d6e7f8..91ecb94") {
		t.Fatalf("title = %q, want the span size and its ends, oldest first", title)
	}
	l.apply(key{kind: keyUp})
	if title := stripSGR(l.render(100)[0]); !strings.Contains(title, "1 change") || strings.Contains(title, "1 changes") {
		t.Fatalf("title = %q, want a singular count", title)
	}
	l.apply(key{kind: keyDown})
	rows := l.render(100)[2:5]
	for _, row := range rows[:2] {
		if !strings.Contains(row, seq(th.Selected)) {
			t.Errorf("row = %q, want it lit inside the span", row)
		}
	}
	if strings.Contains(rows[2], seq(th.Selected)) {
		t.Errorf("row = %q, want it outside the span", rows[2])
	}
	if !strings.Contains(rows[1], cursorMarker) || strings.Contains(rows[0], cursorMarker) {
		t.Errorf("rows = %q / %q, want the marker on the cursor row only", rows[0], rows[1])
	}
}

// A range covers every item between its ends, so a view that narrows the
// rows is set aside while one is open: a row the view hides would end up
// inside a range without ever being shown.
func TestSelectRangeSetsANarrowingViewAside(t *testing.T) {
	opts := Options{
		Items: []Item{
			{Cells: []string{"c1"}, Groups: []string{"mine"}},
			{Cells: []string{"c2"}, Groups: []string{"theirs"}},
			{Cells: []string{"c3"}, Groups: []string{"mine"}},
		},
		Views: []View{{Name: "mine", Groups: []string{"mine"}}, {Name: "all"}},
		Range: true,
	}
	l := newList(opts, 24, false)
	if len(l.matches) != 2 {
		t.Fatalf("matches = %v, want the view to have narrowed the list", l.matches)
	}
	l.apply(key{kind: keyDown})
	l.apply(key{kind: keyEnter})
	if len(l.matches) != 3 || l.selected() != 2 {
		t.Fatalf("matches %v, selected %d; want every row shown and the cursor still on c3", l.matches, l.selected())
	}
	if got := l.position(); !strings.Contains(got, " · mine · all") {
		t.Errorf("position() = %q, want no view marked as in force", got)
	}
	l.apply(key{kind: keyUp})
	if first, last := l.selectedRange(); first != 1 || last != 2 {
		t.Errorf("range = %d..%d, want c2 to c3, both on screen", first, last)
	}
	// Tab does not switch while the range is open, and Esc brings the view
	// back with the cursor on a row it shows.
	l.apply(key{kind: keyTab})
	l.apply(key{kind: keyUp})
	l.apply(key{kind: keyBack})
	if len(l.matches) != 2 || l.view != 0 || l.selected() != 0 {
		t.Errorf("after Esc: matches %v, view %d, selected %d; want the narrowed view back, on c1", l.matches, l.view, l.selected())
	}
	if got := l.position(); !strings.Contains(got, "[mine]") {
		t.Errorf("position() = %q, want the view in force again", got)
	}

	// The ends handed back are two rows that were on screen with everything
	// between them.
	first, last, err := driveRange(t, opts, "\r", "\x1b[B", "\x1b[B", "\r")
	if err != nil || first != 0 || last != 2 {
		t.Errorf("range = %d..%d, err %v; want 0..2 with c2 shown in between", first, last, err)
	}
	if first, last, err := driveRange(t, opts, "\r", "\x1b[B", "\r"); err != nil || first != 0 || last != 1 {
		t.Errorf("range = %d..%d, err %v; want one step down to reach c2, not c3", first, last, err)
	}
}
