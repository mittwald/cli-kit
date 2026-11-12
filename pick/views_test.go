package pick

import (
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Tab cycles views that narrow the items by group, and the cursor stays on
// its row wherever that row is still shown.
func TestTabCyclesTheViews(t *testing.T) {
	l := newList(Options{
		Title: "Which one?",
		Items: []Item{
			{Cells: []string{"1.4.26", "install"}, Groups: []string{"install"}},
			{Cells: []string{"1.4.25+1-rc", "edit"}, Groups: []string{"edit"}},
			{Cells: []string{"1.4.24+1-rc", "edit"}, Groups: []string{"edit"}},
		},
		Views: []View{
			{Name: "all"},
			{Name: "edit", Groups: []string{"edit"}},
			{Name: "install", Groups: []string{"install"}},
		},
		Initial:    1,
		MaxVisible: 5,
	}, 0, false)

	for _, step := range []struct {
		matches  []int
		selected int
		view     string
	}{
		{matches: []int{1, 2}, selected: 1, view: "· all · [edit] · install"},
		{matches: []int{0}, selected: 0, view: "· all · edit · [install]"},
		{matches: []int{0, 1, 2}, selected: 0, view: "· [all] · edit · install"},
	} {
		l.apply(key{kind: keyTab})
		if !slices.Equal(l.matches, step.matches) || l.selected() != step.selected {
			t.Errorf("after Tab: matches = %v, selected = %d; want %v, %d", l.matches, l.selected(), step.matches, step.selected)
		}
		if !strings.Contains(l.position(), step.view) {
			t.Errorf("position() = %q, want %s marked", l.position(), step.view)
		}
	}
	if !strings.Contains(strings.Join(l.render(200), "\n"), "Tab view") {
		t.Error("render() does not name the Tab key")
	}
	if got, want := l.position(), "1 of 3     · [all] · edit · install"; got != want {
		t.Errorf("position() = %q, want %q", got, want)
	}
	l.apply(key{kind: keyEnd})
	if got, want := l.position(), "3 of 3     · [all] · edit · install"; got != want {
		t.Errorf("position() = %q, want %q", got, want)
	}
	// Shift-Tab and the arrows go the other way and back.
	l.apply(key{kind: keyBackTab})
	if !strings.Contains(l.position(), "[install]") {
		t.Errorf("after Shift-Tab: position() = %q, want the last view", l.position())
	}
	l.apply(key{kind: keyRight})
	l.apply(key{kind: keyLeft})
	l.apply(key{kind: keyRight})
	if !strings.Contains(l.position(), "[all]") {
		t.Errorf("after →←→: position() = %q, want the first view", l.position())
	}
	// Only the view showing escapes the muted style.
	l.color = true
	if line := l.renderPosition(200); !strings.Contains(line, th.Muted.Paint(" · ")+"[all]"+th.Muted.Paint(" · edit")) {
		t.Errorf("renderPosition() = %q, want [all] unstyled between muted parts", line)
	}
	typeText(l, "1.4")
	if line := l.renderPosition(200); !strings.HasSuffix(line, th.Muted.Paint(" · filter: ")+"1.4") {
		t.Errorf("renderPosition() = %q, want the filter unstyled after a muted label", line)
	}

	// A row of several groups shows in the view of each.
	both := newList(Options{
		Items: []Item{
			{Cells: []string{"1.4.26", "install"}, Groups: []string{"install"}},
			{Cells: []string{"1.4.25", "install", "installed ✓"}, Groups: []string{"install", "installed"}},
		},
		Views: []View{{Name: "all"}, {Name: "install", Groups: []string{"install"}}, {Name: "installed", Groups: []string{"installed"}}},
	}, 0, false)
	for _, want := range [][]int{{0, 1}, {1}} {
		both.apply(key{kind: keyTab})
		if !slices.Equal(both.matches, want) {
			t.Errorf("after Tab: matches = %v, want %v", both.matches, want)
		}
	}

	// A single view is no choice, and Tab does nothing.
	single := newList(Options{Items: []Item{{Cells: []string{"a"}}}, Views: []View{{Name: "all"}}}, 0, false)
	if single.apply(key{kind: keyTab}); len(single.views) != 0 || len(single.matches) != 1 {
		t.Errorf("a single view: views = %v, matches = %v", single.views, single.matches)
	}
}

// A list given a lens opens on its filter and view, keeps Initial naming an
// item rather than a position, and is left holding what it was closed on.
func TestLensNarrowsTheListItOpens(t *testing.T) {
	items := []Item{
		{Cells: []string{"1.5.1"}, Groups: []string{"install"}},
		{Cells: []string{"1.4.26"}, Groups: []string{"install"}},
		{Cells: []string{"1.4.25+1-rc"}, Groups: []string{"edit"}},
		{Cells: []string{"1.4.24"}, Groups: []string{"install"}},
	}
	views := []View{
		{Name: "all"},
		{Name: "install", Groups: []string{"install"}},
		{Name: "edit", Groups: []string{"edit"}},
	}
	lens := &Lens{Filter: "1.4", View: "install"}
	l := newList(Options{Items: items, Views: views, Lens: lens, Initial: 3, MaxVisible: 5}, 0, false)
	if !slices.Equal(l.matches, []int{1, 3}) {
		t.Fatalf("matches = %v, want the 1.4 installs", l.matches)
	}
	if l.selected() != 3 {
		t.Errorf("selected = %d, want Initial's item 3", l.selected())
	}

	l = newList(Options{Items: items, Views: views, Lens: lens, Initial: 2, MaxVisible: 5}, 0, false)
	if l.selected() != 1 {
		t.Errorf("selected = %d, want the first row shown", l.selected())
	}

	l.apply(key{kind: keyTab})
	l.apply(key{kind: keyBackspace})
	if got, want := l.lens(Lens{}), (Lens{Filter: "1.", View: "edit"}); got != want {
		t.Errorf("lens() = %+v, want %+v", got, want)
	}

	// A view the list does not offer opens the whole of it, filter kept.
	l = newList(Options{Items: items, Lens: &Lens{Filter: "1.4", View: "merge"}, MaxVisible: 5}, 0, false)
	if !slices.Equal(l.matches, []int{1, 2, 3}) {
		t.Errorf("matches = %v, want every 1.4 row", l.matches)
	}
	if got := l.lens(Lens{View: "merge"}); got != (Lens{Filter: "1.4", View: "merge"}) {
		t.Errorf("lens() = %+v, want the filter, and the view it found left alone", got)
	}
}

func scopedOptions() Options {
	return Options{
		Title: "Pick a branch",
		Views: []View{
			{Name: "local", Items: []Item{
				{Cells: []string{"main"}, Key: "main"},
				{Cells: []string{"feature/login"}, Key: "feature/login"},
			}},
			{Name: "remote", Title: "Pick a remote branch", Items: []Item{
				{Cells: []string{"origin/release"}, Key: "release"},
				{Cells: []string{"origin/feature/login"}, Key: "feature/login"},
				{Cells: []string{"origin/main"}, Key: "main"},
			}},
		},
		MaxVisible: 10,
	}
}

// Views that hold their own rows replace the rows on a switch, keep the
// filter, and land the cursor on the row carrying the same key.
func TestScopedViewSwitchKeepsTheRowAndTheFilter(t *testing.T) {
	l := newList(scopedOptions(), 0, false)
	l.apply(key{kind: keyDown})
	l.apply(key{kind: keyTab})
	if got := l.items[l.selected()].Cells[0]; got != "origin/feature/login" {
		t.Errorf("after Tab the cursor is on %q, want the row with the same key", got)
	}
	if got := l.render(80)[0]; got != "Pick a remote branch" {
		t.Errorf("title = %q, want the view's own title", got)
	}
	typeText(l, "main")
	l.apply(key{kind: keyTab})
	if got, want := l.matches, []int{0}; !slices.Equal(got, want) || string(l.filter) != "main" {
		t.Errorf("after Tab: matches %v, filter %q; want the filter kept", got, string(l.filter))
	}
	if got := l.render(80)[0]; got != "Pick a branch" {
		t.Errorf("title = %q, want the fallback title", got)
	}
	// A row whose key is gone starts the cursor at the top.
	l.apply(key{kind: keyClearFilter})
	l.apply(key{kind: keyTab})
	l.apply(key{kind: keyHome})
	l.apply(key{kind: keyTab})
	if l.cursor != 0 {
		t.Errorf("cursor = %d, want the top for a key the view lacks", l.cursor)
	}
}

// An empty view says so, in the caller's words, and selects nothing.
func TestEmptyScopeSaysSoAndSelectsNothing(t *testing.T) {
	opts := scopedOptions()
	opts.Views = append(opts.Views, View{Name: "stale", Items: []Item{}, Empty: "no stale branches"})
	opts.View = 2
	l := newList(opts, 0, false)
	if !slices.Contains(l.render(80), "  no stale branches") {
		t.Errorf("render() = %q, want the view's empty line", l.render(80))
	}
	if got := l.apply(key{kind: keyEnter}); got != actionNone {
		t.Errorf("Enter in an empty view = %v, want nothing", got)
	}
	if got := l.position(); !strings.HasPrefix(got, "0 of 0") {
		t.Errorf("position() = %q, want 0 of 0", got)
	}
}

// A view's rows can be loaded the first time it is shown, once, after its
// loading line is on screen.
func TestLazyViewLoadsOnceWhenShown(t *testing.T) {
	calls := 0
	opts := scopedOptions()
	opts.Views[1] = View{Name: "remote", Loading: "fetching…", Load: func() ([]Item, error) {
		calls++
		return []Item{{Cells: []string{"origin/main"}, Key: "main"}}, nil
	}}
	l := newList(opts, 0, false)
	if calls != 0 || l.loadPending() {
		t.Fatalf("Load ran %d times before the view was shown, pending %v", calls, l.loadPending())
	}
	l.apply(key{kind: keyTab})
	if !l.loadPending() || !slices.Contains(l.render(80), "  fetching…") {
		t.Fatalf("pending %v, render %q; want the loading line first", l.loadPending(), l.render(80))
	}
	l.runPendingLoad()
	if calls != 1 || len(l.items) != 1 {
		t.Fatalf("calls %d, items %d; want one load with one row", calls, len(l.items))
	}
	l.apply(key{kind: keyTab})
	l.apply(key{kind: keyTab})
	if calls != 1 || l.loadPending() {
		t.Errorf("calls %d, pending %v after coming back; want no second load", calls, l.loadPending())
	}
}

// A failing load says so where the rows would be, and keeps whatever rows it
// did return, with the failure on the position line.
func TestLazyViewShowsItsFailure(t *testing.T) {
	opts := scopedOptions()
	opts.Views[1] = View{Name: "remote", Load: func() ([]Item, error) { return nil, errors.New("remote unreachable") }}
	opts.View = 1
	l := newList(opts, 0, false)
	l.runPendingLoad()
	if !slices.Contains(l.render(80), "  remote unreachable") {
		t.Errorf("render() = %q, want the failure in place of the rows", l.render(80))
	}

	opts.Views[1].Load = func() ([]Item, error) {
		return []Item{{Cells: []string{"origin/main"}}}, errors.New("partial listing")
	}
	l = newList(opts, 0, false)
	l.runPendingLoad()
	if len(l.items) != 1 || !strings.Contains(l.position(), "partial listing") {
		t.Errorf("items %d, position %q; want the rows and the failure", len(l.items), l.position())
	}
}

// The key loop runs a pending load between two draws.
func TestDriveRunsAPendingLoad(t *testing.T) {
	opts := scopedOptions()
	opts.Views[1] = View{Name: "remote", Load: func() ([]Item, error) {
		return []Item{{Cells: []string{"origin/a"}}, {Cells: []string{"origin/b"}}}, nil
	}}
	l := newList(opts, 0, false)
	err := drive(l, &renderer{w: io.Discard}, fixedSize(80, 24), 0, &scriptedInput{chunks: [][]byte{[]byte("\t"), []byte("\x1b[B\r")}})
	if err != nil || l.view != 1 || l.selected() != 1 {
		t.Errorf("drive() = %v, view %d, selected %d; want row 1 of the loaded view", err, l.view, l.selected())
	}
}

// A list whose only rows are in a view yet to load is not empty.
func TestTotalItemsCountsLazyViews(t *testing.T) {
	opts := Options{Views: []View{{Name: "a", Load: func() ([]Item, error) { return nil, nil }}}}
	if opts.totalItems() == 0 {
		t.Error("totalItems() = 0, want a lazy view to count")
	}
	if (Options{Views: []View{{Name: "a", Items: []Item{}}}}).totalItems() != 0 {
		t.Error("totalItems() of empty views is not 0")
	}
}

// Views that hold their own rows cannot be combined with typed answers or
// marking.
func TestScopedViewsRefuseTypedAndMulti(t *testing.T) {
	opts := scopedOptions()
	opts.Typed = func(string) bool { return true }
	if _, _, err := SelectOrType(nil, nil, opts); !errors.Is(err, errScopedChoice) {
		t.Errorf("SelectOrType() error = %v, want errScopedChoice", err)
	}
	if _, _, err := SelectManyOrType(nil, nil, scopedOptions()); !errors.Is(err, errScopedChoice) {
		t.Errorf("SelectManyOrType() error = %v, want errScopedChoice", err)
	}
}

// The entry points check for rows and a terminal before drawing anything.
func TestSelectRejectsEmptyListsAndNonTerminals(t *testing.T) {
	if _, err := Select(nil, nil, Options{}); !errors.Is(err, ErrNoItems) {
		t.Errorf("Select() of nothing = %v, want ErrNoItems", err)
	}
	if _, _, err := SelectView(nil, nil, Options{Views: []View{{Items: []Item{}}}}); !errors.Is(err, ErrNoItems) {
		t.Errorf("SelectView() of empty views = %v, want ErrNoItems", err)
	}
	opts := Options{Items: []Item{{Cells: []string{"a"}}}}
	if _, err := Select(nil, nil, opts); !errors.Is(err, ErrNotATerminal) {
		t.Errorf("Select() without a terminal = %v, want ErrNotATerminal", err)
	}
	if _, _, err := SelectOrType(nil, nil, Options{}); !errors.Is(err, ErrNoItems) {
		t.Errorf("SelectOrType() of nothing = %v, want ErrNoItems", err)
	}
	if _, err := Input(nil, nil, InputOptions{}); !errors.Is(err, ErrNotATerminal) {
		t.Errorf("Input() without a terminal = %v, want ErrNotATerminal", err)
	}
	if _, err := Toggles(nil, nil, ToggleOptions{}); !errors.Is(err, ErrNoItems) {
		t.Errorf("Toggles() of nothing = %v, want ErrNoItems", err)
	}
	if _, err := Checklist(nil, nil, ChecklistOptions{Boxes: []Box{{Label: "a"}}}); !errors.Is(err, ErrNotATerminal) {
		t.Errorf("Checklist() without a terminal = %v, want ErrNotATerminal", err)
	}
}

// A view loaded on its first switch keeps the cursor on the row with the
// key that was selected, as a view that already holds its rows does.
func TestLazyViewKeepsTheSelectedKey(t *testing.T) {
	opts := scopedOptions()
	opts.Views[1] = View{Name: "remote", Load: func() ([]Item, error) {
		return []Item{
			{Cells: []string{"origin/release"}, Key: "release"},
			{Cells: []string{"origin/feature/login"}, Key: "feature/login"},
		}, nil
	}}
	l := newList(opts, 0, false)
	l.apply(key{kind: keyDown})
	l.apply(key{kind: keyTab})
	l.runPendingLoad()
	if got := l.items[l.selected()].Cells[0]; got != "origin/feature/login" {
		t.Errorf("after the load the cursor is on %q, want the row with the same key", got)
	}
}

// Views that narrow the items by group show their own title too.
func TestGroupViewsShowTheirTitle(t *testing.T) {
	l := newList(Options{
		Title: "Which one?",
		Items: []Item{
			{Cells: []string{"1.4.26"}, Groups: []string{"install"}},
			{Cells: []string{"1.4.25"}, Groups: []string{"edit"}},
		},
		Views: []View{
			{Name: "all"},
			{Name: "edit", Groups: []string{"edit"}, Title: "Which version to edit?"},
		},
		View: 1,
	}, 0, false)
	if got := l.render(80)[0]; !strings.HasPrefix(got, "Which version to edit?") {
		t.Errorf("title = %q, want the initial view's title", got)
	}
	l.apply(key{kind: keyTab})
	if got := l.render(80)[0]; !strings.HasPrefix(got, "Which one?") {
		t.Errorf("title = %q, want the options' title for a view without one", got)
	}
	l.apply(key{kind: keyTab})
	if got := l.render(80)[0]; !strings.HasPrefix(got, "Which version to edit?") {
		t.Errorf("title = %q, want the view's title after Tab", got)
	}
}

// A lens that names no view leaves the view the options ask for, and a view
// without a name is found again by its position, the way the status line
// labels it.
func TestLensFindsAViewWithoutAName(t *testing.T) {
	items := []Item{{Cells: []string{"a"}, Groups: []string{"g1"}}, {Cells: []string{"b"}, Groups: []string{"g2"}}}
	views := []View{{Groups: []string{"g1"}}, {Groups: []string{"g2"}}, {}}

	if l := newList(Options{Items: items, Views: views, Lens: &Lens{}}, 0, false); l.view != 0 {
		t.Errorf("an empty lens opened view %d, want the first", l.view)
	}
	if l := newList(Options{Items: items, Views: views, View: 1, Lens: &Lens{Filter: "b"}}, 0, false); l.view != 1 {
		t.Errorf("a lens with a filter only opened view %d, want Options.View", l.view)
	}

	// What a list is closed on is what the next one opens on.
	for view := range views {
		l := newList(Options{Items: items, Views: views, View: view}, 0, false)
		lens := l.lens(Lens{})
		if want := strconv.Itoa(view + 1); lens.View != want {
			t.Errorf("view %d: lens().View = %q, want its position %q", view, lens.View, want)
		}
		if reopened := newList(Options{Items: items, Views: views, Lens: &lens}, 0, false); reopened.view != view {
			t.Errorf("view %d: reopened on view %d", view, reopened.view)
		}
	}

	// Among views of the same name the first one is meant.
	named := []View{{Name: "x", Groups: []string{"g1"}}, {Name: "x", Groups: []string{"g2"}}}
	if l := newList(Options{Items: items, Views: named, Lens: &Lens{View: "x"}}, 0, false); l.view != 0 {
		t.Errorf("a name two views share opened view %d, want the first", l.view)
	}
}

// Options.Initial names an item of the view the list opens on, also when
// that view's rows arrive only once it has loaded them.
func TestInitialOnAViewThatLoadsItsRows(t *testing.T) {
	rows := []Item{{Cells: []string{"a"}}, {Cells: []string{"b"}}, {Cells: []string{"c"}}}
	opts := Options{Initial: 2, Views: []View{
		{Name: "remote", Load: func() ([]Item, error) { return rows, nil }},
		{Name: "local", Items: rows},
	}}
	l := newList(opts, 0, false)
	l.runPendingLoad()
	if got := l.selected(); got != 2 {
		t.Errorf("selected() = %d once the rows are there, want Initial's 2", got)
	}

	// Leaving the view first makes its rows arrive as any other view's.
	l = newList(opts, 0, false)
	work := l.nextLoad()
	l.apply(key{kind: keyTab})
	work()()
	l.apply(key{kind: keyTab})
	if got := l.selected(); got != 0 {
		t.Errorf("selected() = %d after a switch away and back, want the first row", got)
	}
}

// loadingList is a list that opens on a view whose Load blocks until release
// is closed, and the channel started that Load closes once it runs.
func loadingList() (l *list, started, release chan struct{}) {
	started, release = make(chan struct{}), make(chan struct{})
	opts := scopedOptions()
	opts.Views[1] = View{Name: "remote", Load: func() ([]Item, error) {
		close(started)
		<-release
		return []Item{{Cells: []string{"origin/a"}}, {Cells: []string{"origin/b"}}}, nil
	}}
	opts.View = 1
	return newList(opts, 0, false), started, release
}

// A view loads its rows off the key loop, so the list takes keys meanwhile:
// Esc and Ctrl-C leave it while Load is still running.
func TestLoadCanBeLeftWhileItRuns(t *testing.T) {
	for _, c := range []struct {
		name string
		key  byte
		want error
	}{
		{"Esc", 0x1b, ErrBack},
		{"Ctrl-C", 0x03, ErrInterrupted},
	} {
		t.Run(c.name, func(t *testing.T) {
			l, started, release := loadingList()
			defer close(release)
			// The key is typed once Load is seen running.
			source := newWakeableInput(nil, []byte{c.key})
			source.blocked = func() {
				<-started
				source.wake()
			}
			err := drive(l, &renderer{w: io.Discard}, fixedSize(80, 24), 0, source)
			if !errors.Is(err, c.want) {
				t.Fatalf("drive() error = %v, want %v", err, c.want)
			}
			if l.loads[1] != loadRunning || len(l.items) != 0 {
				t.Errorf("load state %d with %d rows; want the list left before Load returned", l.loads[1], len(l.items))
			}
		})
	}
}

// The rows a view loaded go on screen as soon as they are there, and the
// keys typed after that act on them.
func TestLoadedRowsArriveWhileTheListWaitsForAKey(t *testing.T) {
	l, _, release := loadingList()
	var out strings.Builder
	source := newWakeableInput(nil, []byte("\x1b[B\r"))
	source.blocked = func() { close(release) }
	err := drive(l, &renderer{w: &out}, fixedSize(80, 24), 0, source)
	if err != nil || l.selected() != 1 {
		t.Fatalf("drive() = %v, selected %d; want row 1 of the loaded view", err, l.selected())
	}
	loading, loaded := strings.Index(out.String(), "loading…"), strings.Index(out.String(), "origin/b")
	if loading < 0 || loaded < loading {
		t.Errorf("drawn %q; want the loading line first and the rows after it", out.String())
	}

	// Rows that arrive after the list moved on to another view are kept for
	// when it comes back.
	l, started, release := loadingList()
	source = newWakeableInput([]byte("\t"), nil, []byte("\t"), []byte("\r"))
	source.blocked = func() {
		<-started
		close(release)
	}
	err = drive(l, &renderer{w: io.Discard}, fixedSize(80, 24), 0, source)
	if err != nil || l.view != 1 || len(l.items) != 2 || l.selected() != 0 {
		t.Errorf("drive() = %v, view %d, %d rows, selected %d; want the loaded view with its rows", err, l.view, len(l.items), l.selected())
	}
}

// A panic in Load is raised again on the key loop, where the prompt's
// deferred cleanup puts the terminal back before the program ends.
func TestLoadPanicReachesTheKeyLoop(t *testing.T) {
	opts := scopedOptions()
	opts.Views[1] = View{Name: "remote", Load: func() ([]Item, error) { panic("boom") }}
	opts.View = 1
	l := newList(opts, 0, false)
	defer func() {
		if got := recover(); got != "boom" {
			t.Errorf("recovered %v, want the panic Load raised", got)
		}
	}()
	_ = drive(l, &renderer{w: io.Discard}, fixedSize(80, 24), 0, newWakeableInput(nil))
	t.Error("drive() returned")
}
