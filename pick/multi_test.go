package pick

import (
	"slices"
	"strings"
	"testing"
)

// A list that takes several rows marks them with Space, the cursor staying
// on the row marked, and answers with every row marked, in list order. A
// disabled row is not marked.
func TestSpaceMarksSeveralRows(t *testing.T) {
	l := newList(Options{
		Items: []Item{
			{Cells: []string{"1.4.25"}, Action: "create"},
			{Cells: []string{"1.4.24"}, Disabled: true},
			{Cells: []string{"1.3.30"}, Action: "recreate"},
		},
		multi: true,
	}, 0, false)

	// The boxes appear with the first Space, on every row at once, in a lane
	// kept for them, so no row moves.
	if got, want := l.render(100)[2:5], []string{"❯     1.4.25  create", "      1.4.24", "      1.3.30  recreate"}; !slices.Equal(got, want) {
		t.Errorf("rows before marking =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	l.apply(key{kind: keyEnd})
	l.apply(key{kind: keyRune, rune: ' '})
	if got, want := l.render(100)[2:5], []string{"  [ ] 1.4.25  create", "  [ ] 1.4.24", "❯ [x] 1.3.30  recreate"}; !slices.Equal(got, want) {
		t.Errorf("rows after the first Space =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	l.apply(key{kind: keyHome})
	l.apply(key{kind: keyRune, rune: ' '})
	if got := l.selected(); got != 0 {
		t.Fatalf("selected() after marking = %d, want the cursor left on 0", got)
	}
	l.apply(key{kind: keyDown})
	l.apply(key{kind: keyRune, rune: ' '})
	if len(l.filter) > 0 || len(l.marked) != 2 {
		t.Fatalf("Space on a disabled row: filter %q, marked %v; want neither changed", string(l.filter), l.marked)
	}

	lines := l.render(100)
	want := []string{"  [x] 1.4.25  create", "❯ [ ] 1.4.24", "  [x] 1.3.30  recreate"}
	if got := lines[2:5]; !slices.Equal(got, want) {
		t.Errorf("rows =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if got, want := l.position(), "2 of 3 · 2 marked · nothing to do"; got != want {
		t.Errorf("position() = %q, want %q", got, want)
	}
	if got := lines[len(lines)-1]; !strings.Contains(got, "Space mark") {
		t.Errorf("hint = %q, want it to name the mark key", got)
	}
	// Enter answers with the marks, though the row under the cursor could not
	// be chosen by itself.
	if got := l.apply(key{kind: keyEnter}); got != actionSelect {
		t.Fatalf("apply(Enter) = %v, want the marked rows chosen", got)
	}
	if indexes, typed := l.chosen(); !slices.Equal(indexes, []int{0, 2}) || typed != nil {
		t.Errorf("chosen() = %v %q, want rows 0 and 2", indexes, typed)
	}

	// Space again unmarks.
	l.apply(key{kind: keyHome})
	l.apply(key{kind: keyRune, rune: ' '})
	if indexes, _ := l.chosen(); !slices.Equal(indexes, []int{2}) {
		t.Errorf("chosen() after unmarking = %v, want row 2 alone", indexes)
	}
}

// With nothing marked, Enter answers with the row under the cursor, typed row
// included.
func TestNothingMarkedAnswersTheCursor(t *testing.T) {
	l := newList(Options{
		Items: []Item{{Cells: []string{"1.4.25"}}, {Cells: []string{"1.4.24"}}},
		Typed: func(string) bool { return true },
		multi: true,
	}, 0, false)
	l.move(1)
	if indexes, typed := l.chosen(); !slices.Equal(indexes, []int{1}) || typed != nil {
		t.Errorf("chosen() = %v %q, want the row under the cursor", indexes, typed)
	}
	typeText(l, "1.4.26")
	if indexes, typed := l.chosen(); indexes != nil || !slices.Equal(typed, []string{"1.4.26"}) {
		t.Errorf("chosen() = %v %q, want what was typed", indexes, typed)
	}
}

// A typed row marked becomes a row of its own, kept when the filter that
// offered it changes, and shown whatever the view.
func TestMarkedTypedRowStays(t *testing.T) {
	items := []Item{{Cells: []string{"1.4.25"}, Groups: []string{"untagged"}}, {Cells: []string{"1.4.24"}, Groups: []string{"untagged"}}}
	l := newList(Options{
		Items:     items,
		Typed:     func(string) bool { return true },
		TypedNote: "typed",
		Views:     []View{{Name: "all"}, {Name: "tagged", Groups: []string{"tagged"}}},
		multi:     true,
	}, 0, false)
	typeText(l, "1.4.26")
	l.apply(key{kind: keyRune, rune: ' '})
	if len(items) != 2 || l.offersTyped() {
		t.Fatalf("marking the typed row: caller's items %d, typed row still offered %v", len(items), l.offersTyped())
	}
	l.apply(key{kind: keyClearFilter})
	typeText(l, "1.4.27")
	l.apply(key{kind: keyRune, rune: ' '})
	l.apply(key{kind: keyClearFilter})
	l.apply(key{kind: keyTab})
	if got, want := l.matches, []int{2, 3}; !slices.Equal(got, want) {
		t.Errorf("matches in the tagged view = %v, want the two typed rows", got)
	}
	if indexes, typed := l.chosen(); indexes != nil || !slices.Equal(typed, []string{"1.4.26", "1.4.27"}) {
		t.Errorf("chosen() = %v %q, want both typed versions in the order marked", indexes, typed)
	}
}

// Only a list that takes several rows names the mark key, and it does so
// within the width.
func TestHintNamesTheMarkKeyOnlyWhereItMarks(t *testing.T) {
	for _, views := range []bool{false, true} {
		opts := Options{Items: []Item{{Cells: []string{"a"}, Groups: []string{"x"}}}}
		if views {
			opts.Views = []View{{Name: "all"}, {Name: "x", Groups: []string{"x"}}}
		}
		if got := newList(opts, 0, false).hint(200); strings.Contains(got, "Space") {
			t.Errorf("hint(views %v) = %q, want no mark key", views, got)
		}
		opts.multi = true
		l := newList(opts, 0, false)
		for _, n := range []int{200, 70} {
			if got := l.hint(n); !strings.Contains(got, "Space mark · type to filter") || width(got) > n {
				t.Errorf("hint(%d, views %v, multi) = %q, want the mark key within the width", n, views, got)
			}
		}
	}
}
