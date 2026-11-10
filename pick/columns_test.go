package pick

import (
	"slices"
	"strings"
	"testing"

	"github.com/mittwald/cli-kit/theme"
)

// fitRow is fitWidths for a row of n cells with the cursor marker and the
// normal gap between every pair of columns.
func fitRow(widths, priorities []int, n int) []int {
	return fitWidths(widths, priorities, n-width(cursorMarker)-(len(widths)-1)*len(columnGap))
}

func rowWidth(widths []int) int {
	total := width(cursorMarker) + (len(widths)-1)*len(columnGap)
	for _, w := range widths {
		total += w
	}
	return total
}

func TestFitWidthsShrinksTheWidestColumn(t *testing.T) {
	got := fitRow([]int{4, 60, 6, 3}, nil, 60)
	if got[0] != 4 || got[2] != 6 || got[3] != 3 {
		t.Fatalf("fitted = %v, want the narrow columns untouched", got)
	}
	if total := rowWidth(got); total > 60 {
		t.Fatalf("fitted row width = %d, want it within 60: %v", total, got)
	}
	// Nothing shrinks below the readable floor: cutting the line takes over
	// rather than the loop spinning.
	if got := fitRow([]int{8, 8, 8}, nil, 10); len(got) != 3 || got[0] != 8 {
		t.Fatalf("fitted = %v, want the floors kept", got)
	}
	if got := fitRow(nil, nil, 80); got != nil {
		t.Fatalf("fitted = %v, want nil for no columns", got)
	}
}

// Two long columns end up comparable rather than one keeping its full width
// while the other is cut to the floor.
func TestFitWidthsLevelsWideColumns(t *testing.T) {
	got := fitRow([]int{43, 60, 13, 3}, nil, 80)
	if diff := got[0] - got[1]; diff > 1 || diff < -1 {
		t.Fatalf("fitted = %v, want the two wide columns leveled", got)
	}
	if got[2] != 13 || got[3] != 3 {
		t.Fatalf("fitted = %v, want the narrow columns untouched", got)
	}
	if total := rowWidth(got); total > 80 {
		t.Fatalf("row width = %d, want it within 80: %v", total, got)
	}
}

// An unimportant column collapses to its floor before an important one loses
// a cell.
func TestFitWidthsSpendsTheUnimportantColumnFirst(t *testing.T) {
	// marker, name (low), message, author, age
	widths := []int{1, 43, 60, 13, 3}
	priorities := []int{PriorityNormal, PriorityLow}
	got := fitRow(widths, priorities, 80)
	if got[1] != stubColumnWidth {
		t.Fatalf("name column = %d, want it down to its floor %d: %v", got[1], stubColumnWidth, got)
	}
	if small := fitRow(widths, priorities, 125); small[2] != widths[2] || small[1] >= widths[1] {
		t.Fatalf("fitted = %v, want only the name shortened", small)
	}
	if got[3] != 13 || got[0] != 1 || got[4] != 3 {
		t.Fatalf("fitted = %v, want the other columns untouched", got)
	}
	if total := rowWidth(got); total > 80 {
		t.Fatalf("row width = %d, want it within 80: %v", total, got)
	}
	if wide := fitRow(widths, priorities, 160); !slices.Equal(wide, widths) {
		t.Fatalf("fitted = %v, want the full widths on a wide terminal", wide)
	}
	tight := fitRow(widths, priorities, 50)
	if tight[1] != stubColumnWidth || tight[2] >= got[2] {
		t.Fatalf("fitted = %v, want the message to give way only after the name", tight)
	}
	if tight[2] < tight[3] {
		t.Fatalf("fitted = %v, want the message and the author leveled, not one collapsed", tight)
	}
}

func TestColumnPriorityDefaultsToNormal(t *testing.T) {
	if got := columnPriority(nil, 3); got != PriorityNormal {
		t.Fatalf("columnPriority = %d, want PriorityNormal", got)
	}
	if got := columnPriority([]int{PriorityLow}, 0); got != PriorityLow {
		t.Fatalf("columnPriority = %d, want PriorityLow", got)
	}
	if got := columnPriority([]int{PriorityLow}, 2); got != PriorityNormal {
		t.Fatalf("columnPriority past the slice = %d, want PriorityNormal", got)
	}
}

// Shrinking a long middle column keeps the columns after it on screen.
func TestRenderKeepsTrailingColumnsVisible(t *testing.T) {
	l := newList(Options{Title: "t", Items: []Item{
		{Cells: []string{"#142", strings.Repeat("long title ", 12), "alice", "2d"}},
	}}, 24, false)
	row := l.render(80)[2]
	if !strings.Contains(row, "alice") || !strings.Contains(row, "2d") {
		t.Fatalf("row = %q, want the author and the age still on screen", row)
	}
	if width(row) > 80 {
		t.Fatalf("row is %d wide: %q", width(row), row)
	}
}

// A one-cell marker column survives a narrow terminal and sits one space
// from the column it marks.
func TestRenderKeepsTheMarkerVisible(t *testing.T) {
	l := newList(Options{Title: "t", Items: []Item{
		{Cells: []string{"★", "#142", strings.Repeat("long title ", 12), "Alice Example", "37m"}},
		{Cells: []string{"", "#139", strings.Repeat("other title ", 12), "Alice Example", "46m"}},
	}}, 24, false)
	row := l.render(80)[2]
	if !strings.HasPrefix(row, "❯ ★ #142") {
		t.Fatalf("row = %q, want the marker intact and close to its row", row)
	}
	if width(row) > 80 {
		t.Fatalf("row is %d wide: %q", width(row), row)
	}
}

// A column may ask to be cut in the middle, keeping both ends readable.
func TestColumnTruncationCutsTheMiddle(t *testing.T) {
	opts := Options{Items: []Item{{Cells: []string{"x", "service-alpha-production-eu-west-replica-07", "tail"}}}}
	plain := newList(opts, 0, false).render(40)[2]
	opts.ColumnTruncation = []Truncation{TruncateEnd, TruncateMiddle}
	middle := newList(opts, 0, false).render(40)[2]
	if !strings.Contains(plain, "service-alpha") || strings.Contains(plain, "replica-07") {
		t.Errorf("end-cut row = %q, want the start kept", plain)
	}
	if !strings.Contains(middle, "service-al") || !strings.Contains(middle, "ica-07") || !strings.Contains(middle, "…") {
		t.Errorf("middle-cut row = %q, want both ends kept", middle)
	}
	for _, row := range []string{plain, middle} {
		if width(row) > 40 || !strings.HasSuffix(row, "tail") {
			t.Errorf("row = %q (%d cells), want it within 40 and the last column kept", row, width(row))
		}
	}
}

func TestCutMiddleKeepsBothEnds(t *testing.T) {
	for _, c := range []struct {
		in    string
		width int
		want  string
	}{
		{"archivekeeper", 13, "archivekeeper"},
		{"archivekeeper", 7, "arc…per"},
		{"archivekeeper", 6, "arc…er"},
		{"archivekeeper", 2, "a…"},
		{"archivekeeper", 1, "…"},
		{"archivekeeper", 0, ""},
		// A double-width rune never lands half inside the kept ends.
		{"レビューを見る", 5, "レ…る"},
	} {
		got := cutMiddle(c.in, c.width)
		if got != c.want {
			t.Errorf("cutMiddle(%q, %d) = %q, want %q", c.in, c.width, got, c.want)
		}
		if width(got) > c.width {
			t.Errorf("cutMiddle(%q, %d) = %q, wider than asked", c.in, c.width, got)
		}
	}
}

// Columns take the caller's styles; a row's own style wins over its column's,
// a shorter style list leaves the rest unstyled, and the selected row adds
// bold to a styled cell.
func TestCellStyles(t *testing.T) {
	red, green, blue := theme.Fg256(1), theme.Fg256(2), theme.Fg256(4)
	l := newList(Options{
		Items: []Item{
			{Cells: []string{"#142", "first", "alice"}},
			{Cells: []string{"#139", "second", "bob"}, CellStyles: []theme.Style{"", blue}},
		},
		CellStyles: []theme.Style{red, green},
	}, 0, true)
	selected, other := l.renderRow(0, 80), l.renderRow(1, 80)
	if !strings.Contains(selected, theme.Join(theme.Bold, red).Paint("#142")) {
		t.Errorf("selected row = %q, want its first cell red and bold", selected)
	}
	if !strings.Contains(other, red.Paint("#139")) || !strings.Contains(other, blue.Paint("second")) {
		t.Errorf("row = %q, want the column style and the row's own override", other)
	}
	if !strings.Contains(other, th.Qualifier.Paint("bob")) {
		t.Errorf("row = %q, want an unstyled column in the default qualifier style", other)
	}
}

// PaintCell paints inside a cell, can decline a column, and is not called
// for receded rows.
func TestPaintCellHook(t *testing.T) {
	var calls []int
	l := newList(Options{
		Items: []Item{
			{Cells: []string{"main", "release/1.2"}},
			{Cells: []string{"old", "release/0.9"}, Disabled: true},
		},
		PaintCell: func(column int, text string, selected bool) (string, bool) {
			calls = append(calls, column)
			if column != 1 {
				return "", false
			}
			return strings.ReplaceAll(text, "/", th.Muted.Paint("/")), true
		},
	}, 0, true)
	row := l.renderRow(0, 80)
	if !strings.Contains(row, "release"+th.Muted.Paint("/")+"1.2") {
		t.Errorf("row = %q, want the hook's painting", row)
	}
	if !strings.Contains(row, th.Selected.Paint("main")) {
		t.Errorf("row = %q, want a declined column painted the default way", row)
	}
	calls = nil
	_ = l.renderRow(1, 80)
	if len(calls) != 0 {
		t.Errorf("PaintCell called for a disabled row: %v", calls)
	}
}

// The title line carries the detail of the row under the cursor, in the
// detail style, and follows the cursor.
func TestRenderTitleCarriesTheSelectedDetail(t *testing.T) {
	l := newList(Options{
		Title: "Base branch:",
		Items: []Item{
			{Cells: []string{"main"}, Detail: "origin/main"},
			{Cells: []string{"feat/…"}, Detail: "origin/feat/a-long-branch-name"},
		},
	}, 24, true)
	if title := l.render(120)[0]; !strings.HasSuffix(title, " "+th.Detail.Paint("origin/main")) {
		t.Fatalf("title = %q, want the detail in the info style", title)
	}
	l.apply(key{kind: keyDown})
	if title := l.render(120)[0]; !strings.Contains(title, "a-long-branch-name") {
		t.Fatalf("title = %q, want the detail to follow the cursor", title)
	}
	custom := newList(Options{Title: "t", Items: []Item{{Cells: []string{"a"}, Detail: "d"}}, DetailStyle: th.Accent}, 24, true)
	if title := custom.render(80)[0]; !strings.HasSuffix(title, " "+th.Accent.Paint("d")) {
		t.Fatalf("title = %q, want the caller's detail style", title)
	}
	bare := newList(Options{Title: "t", Items: []Item{{Cells: []string{"a"}}}}, 24, true)
	if title := bare.render(80)[0]; title != th.Title.Paint("t") {
		t.Fatalf("title = %q, want no detail", title)
	}
}

// Detail fields carry their own styles behind the separator, and an empty
// field adds nothing.
func TestDetailFieldsCarryTheirOwnStyles(t *testing.T) {
	hash := theme.Fg256(71)
	l := newList(Options{
		Title: "Session",
		Items: []Item{{
			Cells: []string{"57e092c6"},
			Details: []Field{
				{Text: "57e092c6-e0f9-4b14-9b88-00b3cacb410a", Style: hash},
				{Text: "/src/project/cmd"},
				{Text: "", Style: th.Muted},
			},
		}},
	}, 24, true)
	header := l.render(120)[0]
	if !strings.Contains(header, hash.Paint("57e092c6-e0f9-4b14-9b88-00b3cacb410a")) {
		t.Fatalf("header = %q, want the id in its own style", header)
	}
	if got, want := stripSGR(header), "Session · 57e092c6-e0f9-4b14-9b88-00b3cacb410a · /src/project/cmd"; got != want {
		t.Fatalf("header = %q, want %q", got, want)
	}
}

func TestMatchFieldsHonorTheirMinimumTerm(t *testing.T) {
	items := []Item{
		{Cells: []string{"row one"}, MatchFields: []MatchField{
			{Text: "feat/abc"},
			{Text: "3f1a9c22-0000-4000-8000-000000000001", MinTerm: 4},
		}},
		{Cells: []string{"row two"}, MatchFields: []MatchField{
			{Text: "main"},
			{Text: "aaaabbbb-0000-4000-8000-000000000002", MinTerm: 4},
		}},
	}
	l := newList(Options{Items: items}, 24, false)
	matched := func(filter string) []int {
		l.setFilter([]rune(filter))
		return append([]int(nil), l.matches...)
	}
	if got := matched("3f1a"); !slices.Equal(got, []int{0}) {
		t.Fatalf("matches for an id term = %v, want the first row", got)
	}
	if got := matched("3f1"); len(got) != 0 {
		t.Fatalf("matches for a short term = %v, want none", got)
	}
	if got := matched("abc"); !slices.Equal(got, []int{0}) {
		t.Fatalf("matches for a short term in an open field = %v, want the first row", got)
	}
	if got := matched("feat 3f1a"); !slices.Equal(got, []int{0}) {
		t.Fatalf("matches for two terms = %v, want the row carrying both", got)
	}
	if got := matched("main 3f1a"); len(got) != 0 {
		t.Fatalf("matches for terms from two rows = %v, want none", got)
	}
}
