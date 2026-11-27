package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mittwald/cli-kit/theme"
)

func screenOf(t *testing.T, h *Headless) string {
	t.Helper()
	return h.Screen()
}

func wantContains(t *testing.T, h *Headless, parts ...string) {
	t.Helper()
	s := screenOf(t, h)
	for _, p := range parts {
		if !strings.Contains(s, p) {
			t.Errorf("screen lacks %q:\n%s", p, s)
		}
	}
}

func wantLacks(t *testing.T, h *Headless, parts ...string) {
	t.Helper()
	s := screenOf(t, h)
	for _, p := range parts {
		if strings.Contains(s, p) {
			t.Errorf("screen has %q:\n%s", p, s)
		}
	}
}

func TestCanvasClipsAndKeepsWideCharactersWhole(t *testing.T) {
	b := newBuffer(6, 2)
	c := Canvas{f: &frame{buf: b}, w: 6, h: 2}
	c.Text(0, 0, "ab日本語", "")
	c.Sub(1, 1, 3, 1).Text(0, 0, "xyzw", "")
	got := b.lines()
	if got[0] != "ab日本" || got[1] != " xyz" {
		t.Errorf("lines = %q", got)
	}
	// Overwriting the right half of a wide character blanks its left half.
	c.Text(3, 0, "-", "")
	if got := b.lines()[0]; got != "ab -本" {
		t.Errorf("after overwrite = %q", got)
	}
	c.Text(0, 1, "a\x1b[31mb\tc", "")
	if got := b.lines()[1]; got != "a[31mb" {
		t.Errorf("control characters = %q", got)
	}
}

func TestBufferDiffWritesOnlyChanges(t *testing.T) {
	a := newBuffer(4, 2)
	b := newBuffer(4, 2)
	Canvas{f: &frame{buf: b}, w: 4, h: 2}.Text(1, 1, "x", "1")
	out := b.diff(a, true)
	if out != "\x1b[2;2H\x1b[0;1mx\x1b[0m" {
		t.Errorf("diff = %q", out)
	}
	if out := b.diff(b, true); out != "" {
		t.Errorf("diff of the same = %q", out)
	}
	c := newBuffer(3, 1)
	Canvas{f: &frame{buf: c}, w: 3, h: 1}.Text(0, 0, "x", "7;38;5;141;48;2;1;2;3;4")
	if out := c.diff(nil, false); !strings.HasPrefix(out, "\x1b[H\x1b[2J") || !strings.Contains(out, "\x1b[0;7;4mx") || strings.Contains(out, "38") {
		t.Errorf("full plain diff = %q", out)
	}
}

func TestWrapLine(t *testing.T) {
	got := WrapLine(Text("the quick brown fox jumps", ""), 10)
	var lines []string
	for _, l := range got {
		lines = append(lines, l.String())
	}
	if strings.Join(lines, "|") != "the quick|brown fox|jumps" {
		t.Errorf("wrapped = %q", lines)
	}
	got = WrapLine(Text("abcdefghijkl", ""), 5)
	if len(got) != 3 || got[2].String() != "kl" {
		t.Errorf("long word = %v", got)
	}
}

func newListApp() (*App, *List[string], *[]string) {
	chosen := &[]string{}
	l := NewList(func(s string) []Span { return []Span{{Text: s}} })
	l.SetItems([]string{"alpha", "beta", "gamma", "delta"})
	l.OnSelect = func(s string) { *chosen = append(*chosen, s) }
	app := New(VBox(Grow(NewPanel("Items", l)), Fit(NewLegend())))
	return app, l, chosen
}

func TestListMovesFiltersAndChooses(t *testing.T) {
	app, l, chosen := newListApp()
	h := NewHeadless(app, 40, 10)
	wantContains(t, h, "alpha", "delta", "move", "open")
	h.Press("j", "j", "enter")
	if got := strings.Join(*chosen, ","); got != "gamma" {
		t.Errorf("chosen = %q", got)
	}
	h.Press("/")
	h.Type("ta")
	wantContains(t, h, "beta", "delta", "/ta")
	wantLacks(t, h, "alpha", "gamma")
	// While the filter takes text, "j" is typed rather than a move.
	h.Type("j")
	if l.Filter() != "taj" {
		t.Errorf("filter = %q", l.Filter())
	}
	h.Press("backspace", "enter", "G", "enter")
	if got := strings.Join(*chosen, ","); got != "gamma,delta" {
		t.Errorf("chosen = %q", got)
	}
	h.Press("esc")
	wantContains(t, h, "alpha")
	if l.Filter() != "" {
		t.Errorf("esc kept the filter %q", l.Filter())
	}
}

func TestListMarks(t *testing.T) {
	app, l, _ := newListApp()
	h := NewHeadless(app, 40, 10)
	h.Press("space", "space")
	got := l.Marked()
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Errorf("marked = %v", got)
	}
	h.Press("esc")
	if got := l.Marked(); len(got) != 1 || got[0] != "gamma" {
		t.Errorf("after esc marked = %v, want the cursor's", got)
	}
	h.Press("ctrl+a")
	if got := l.Marked(); len(got) != 4 {
		t.Errorf("ctrl+a marked %v", got)
	}
}

func TestListTableAndMouse(t *testing.T) {
	type row struct{ id, name string }
	l := NewList(func(r row) []Span { return []Span{{Text: r.id}, {Text: r.name}} }).
		SetColumns(Column{Title: "ID"}, Column{Title: "NAME", Grow: 1})
	l.SetItems([]row{{"1", "one"}, {"22", "two"}, {"333", "three"}})
	app := New(l, WithMouse())
	h := NewHeadless(app, 30, 6)
	lines := h.Lines()
	if !strings.HasPrefix(lines[0], "  ID  NAME") || !strings.HasPrefix(lines[2], "  22  two") {
		t.Errorf("table:\n%s", h.Screen())
	}
	h.Click(5, 3)
	if r, _ := l.Selected(); r.id != "333" {
		t.Errorf("clicked row = %v", r)
	}
	h.Wheel(5, 2, -1)
	if l.Cursor() != 0 {
		t.Errorf("wheel up cursor = %d", l.Cursor())
	}
}

func TestInputEditsAndSubmits(t *testing.T) {
	in := NewInput()
	var submitted string
	in.OnSubmit = func(s string) { submitted = s }
	app := New(in)
	h := NewHeadless(app, 20, 1)
	h.Type("hello wörld")
	h.Press("ctrl+w")
	h.Type("there")
	h.Press("home", "right", "delete")
	h.Type("E")
	if in.Value() != "hEllo there" {
		t.Errorf("value = %q", in.Value())
	}
	h.Press("ctrl+z")
	if in.Value() != "hllo there" {
		t.Errorf("after undo = %q", in.Value())
	}
	h.Press("enter")
	if submitted != "hllo there" {
		t.Errorf("submitted = %q", submitted)
	}
	if x, y := h.Cursor(); x != 1 || y != 0 {
		t.Errorf("cursor at %d,%d", x, y)
	}
	h.Paste("a\nb")
	if in.Value() != "ha bllo there" {
		t.Errorf("after paste = %q", in.Value())
	}
}

func TestInputScrollsAndSuggests(t *testing.T) {
	in := NewInput()
	in.SetSuggest(func(string) []string { return []string{"kubernetes", "kubectl"} })
	h := NewHeadless(New(in), 8, 1)
	h.Type("kub")
	if got := h.Lines()[0]; got != "kubernet" {
		t.Errorf("ghost = %q", got)
	}
	h.Press("ctrl+n", "end")
	if in.Value() != "kubectl" {
		t.Errorf("taken = %q", in.Value())
	}
	in.SetValue("a long value here")
	h.Press("end")
	if got := h.Lines()[0]; got != "ue here" {
		t.Errorf("scrolled = %q", got)
	}
}

func TestTextAreaWrapsAndMoves(t *testing.T) {
	ta := NewTextArea()
	h := NewHeadless(New(ta), 11, 4)
	h.Type("one two three four")
	h.Press("enter")
	h.Type("x")
	lines := h.Lines()
	if lines[0] != "one two" || lines[1] != "three four" || lines[2] != "x" {
		t.Errorf("rows = %q", lines)
	}
	h.Press("up", "up")
	h.Type("!")
	if got := ta.Value(); got != "o!ne two three four\nx" {
		t.Errorf("value = %q", got)
	}
	h.Press("ctrl+home")
	h.Press("delete")
	if !strings.HasPrefix(ta.Value(), "!ne two") {
		t.Errorf("after delete = %q", ta.Value())
	}
	h.Paste("A\nB")
	if !strings.HasPrefix(ta.Value(), "A\nB!ne two") {
		t.Errorf("after paste = %q", ta.Value())
	}
	h.Press("backspace", "backspace")
	if !strings.HasPrefix(ta.Value(), "A!ne two") {
		t.Errorf("backspace over a line break = %q", ta.Value())
	}
}

func TestViewportScrollsAndSearches(t *testing.T) {
	v := NewViewport()
	var lines []Line
	for i := range 50 {
		lines = append(lines, Text("line "+strings.Repeat("x", i%3)+string(rune('a'+i%26)), ""))
	}
	lines[30] = Text("needle here", "")
	v.SetLines(lines)
	h := NewHeadless(New(v), 20, 5)
	if v.Position() != "Top" {
		t.Errorf("position = %s", v.Position())
	}
	h.Press("G")
	if v.Position() != "Bot" {
		t.Errorf("position after G = %s", v.Position())
	}
	h.Press("g", "/")
	h.Type("needle")
	wantContains(t, h, "/needle")
	h.Press("enter")
	if got := h.Lines()[0]; !strings.HasPrefix(got, "needle here") {
		t.Errorf("after search top line = %q", got)
	}
	h.Press("ctrl+u")
	if v.top != 28 {
		t.Errorf("half page up top = %d", v.top)
	}
}

func TestToggleRadioCheckList(t *testing.T) {
	sw := NewSwitch("notify", false)
	radio := NewRadioGroup([]string{"low", "high"}, 0)
	checks := NewCheckList([]string{"a", "b", "c"})
	app := New(NewForm().Add("Switch", sw).Add("Prio", radio).Add("Labels", checks))
	h := NewHeadless(app, 30, 12)
	h.Press("space")
	if !sw.Value() {
		t.Error("space did not flip the switch")
	}
	h.Press("tab", "j", "enter")
	if radio.Value() != "high" {
		t.Errorf("radio = %q", radio.Value())
	}
	h.Press("tab", "a", "j", "space")
	if got := strings.Join(checks.Values(), ","); got != "a,c" {
		t.Errorf("checks = %q", got)
	}
	wantContains(t, h, "notify  ON", "(•) high", "[x] a", "[ ] b")
}

func TestFormValidatesAndSubmits(t *testing.T) {
	in := NewInput()
	in.Validate = func(s string) error {
		if s == "" {
			return errors.New("required")
		}
		return nil
	}
	in.SetValue("")
	submitted := 0
	form := NewForm().AddHint("Name", in, "your name")
	form.OnSubmit = func() { submitted++ }
	h := NewHeadless(New(form), 30, 6)
	wantContains(t, h, "Name", "required", "your name")
	h.Press("ctrl+s")
	if submitted != 0 {
		t.Error("submitted with an error")
	}
	h.Type("Ada")
	h.Press("ctrl+s")
	if submitted != 1 {
		t.Error("did not submit")
	}
	wantLacks(t, h, "required")
}

func TestLayersConfirmMenuChoosePrompt(t *testing.T) {
	root := NewLabel("root", "")
	app := New(root)
	h := NewHeadless(app, 50, 14)
	yes := 0
	app.Confirm("Delete", "Really delete it?", func() { yes++ })
	h.Press("n")
	if yes != 0 || app.Layers() != 0 {
		t.Errorf("n: yes=%d layers=%d", yes, app.Layers())
	}
	app.Confirm("Delete", "Really delete it?", func() { yes++ })
	h.Press("enter")
	if yes != 1 || app.Layers() != 0 {
		t.Errorf("enter: yes=%d layers=%d", yes, app.Layers())
	}

	picked := ""
	app.Menu("Actions", []MenuItem{
		{Key: "o", Label: "Open", Action: func() { picked = "open" }},
		{Separator: true},
		{Key: "d", Label: "Delete", Disabled: true, Action: func() { picked = "delete" }},
		{Key: "c", Label: "Close", Action: func() { picked = "close" }},
	})
	h.Draw()
	wantContains(t, h, "Open", "Close")
	h.Press("d")
	if picked != "" {
		t.Error("picked a disabled item")
	}
	h.Press("down", "enter")
	if picked != "close" || app.Layers() != 0 {
		t.Errorf("picked %q, layers %d", picked, app.Layers())
	}

	chosen := -1
	app.Choose("Status", []string{"open", "closed"}, 0, func(i int) { chosen = i })
	h.Press("j", "enter")
	if chosen != 1 {
		t.Errorf("chosen = %d", chosen)
	}

	got := ""
	app.Prompt("Rename", "Title", "old", func(s string) { got = s })
	h.Press("ctrl+u")
	h.Type("new")
	h.Press("enter")
	if got != "new" || app.Layers() != 0 {
		t.Errorf("prompt = %q, layers %d", got, app.Layers())
	}

	app.Prompt("Rename", "Title", "x", func(s string) { got = s })
	h.Press("esc")
	if got != "new" || app.Layers() != 0 {
		t.Errorf("esc: prompt = %q, layers %d", got, app.Layers())
	}
}

func TestFocusMovesWithTab(t *testing.T) {
	a, b := NewInput(), NewInput()
	app := New(VBox(Fit(a), Fit(b)))
	h := NewHeadless(app, 10, 2)
	h.Type("1")
	h.Press("tab")
	h.Type("2")
	h.Press("shift+tab")
	h.Type("3")
	if a.Value() != "13" || b.Value() != "2" {
		t.Errorf("a=%q b=%q", a.Value(), b.Value())
	}
}

func TestLegendLeavesOutKeysAFieldTakes(t *testing.T) {
	in := NewInput()
	km := NewKeyed(VBox(Fit(in), Fit(NewLegend())))
	km.Keymap().Bind("quit", func() {}, "q")
	km.Keymap().Bind("send", func() {}, "ctrl+s")
	app := New(km)
	h := NewHeadless(app, 60, 2)
	wantContains(t, h, "^s send", "f1 help")
	wantLacks(t, h, "q quit", "?/f1")
}

func TestHelpListsBindings(t *testing.T) {
	app, _, _ := newListApp()
	h := NewHeadless(app, 60, 30)
	h.Press("?")
	wantContains(t, h, "Keys", "List", "half page down", "General")
	h.Press("esc")
	if app.Layers() != 0 {
		t.Error("esc did not close the help")
	}
}

func TestGoAndBusy(t *testing.T) {
	label := NewLabel("waiting", "")
	app := New(VBox(Grow(label), Fit(NewStatusBar(nil, nil))))
	h := NewHeadless(app, 30, 2)
	done := app.Busy("loading")
	h.Draw()
	wantContains(t, h, "loading")
	app.Go(func() func() {
		return func() {
			label.SetText("loaded", "")
			done()
			app.Notify(LevelSuccess, "all good")
		}
	})
	h.Settle(time.Second)
	wantContains(t, h, "loaded", "all good")
	wantLacks(t, h, "loading")
}

func TestPagesAndTabs(t *testing.T) {
	tabs := NewTabs()
	tabs.AddTab("a", "First", NewLabel("page a", ""))
	tabs.AddTab("b", "Second", NewLabel("page b", ""))
	h := NewHeadless(New(tabs), 30, 3)
	wantContains(t, h, "First   Second", "page a")
	h.Press("]")
	wantContains(t, h, "page b")
	h.Press("alt+1")
	wantContains(t, h, "page a")
}

func TestQuitAndFeed(t *testing.T) {
	app := New(NewLabel("x", ""))
	h := NewHeadless(app, 5, 1)
	h.Feed("\x1b[A\x03")
	if quit, err := h.Quit(); !quit || err != nil {
		t.Errorf("ctrl+c: quit=%v err=%v", quit, err)
	}
}

func TestMouseReachesOnlyTheTopLayer(t *testing.T) {
	pressed := 0
	b := NewButton("Root", func() { pressed++ })
	app := New(b, WithMouse())
	h := NewHeadless(app, 60, 20)
	app.Confirm("Sure", "Really?", func() {})
	h.Draw()
	h.Click(1, 0)
	if pressed != 0 || app.Layers() != 1 {
		t.Errorf("a click beside the dialog pressed the root button: %d, layers %d", pressed, app.Layers())
	}
}

func TestDisabledWidgetsIgnoreClicks(t *testing.T) {
	cb := NewCheckbox("x", false).SetDisabled(true)
	h := NewHeadless(New(cb, WithMouse()), 10, 1)
	h.Click(0, 0)
	if cb.Value() {
		t.Error("a disabled checkbox flipped")
	}
}

func TestViewportScrollBottomAfterSetLines(t *testing.T) {
	v := NewViewport()
	h := NewHeadless(New(v), 20, 3)
	var lines []Line
	for i := range 20 {
		lines = append(lines, Text(strings.Repeat("x", i+1), ""))
	}
	v.SetLines(lines)
	v.ScrollBottom()
	h.Draw()
	if v.Position() != "Bot" {
		t.Errorf("position = %s", v.Position())
	}
}

func TestUndoAfterSetValue(t *testing.T) {
	in := NewInput()
	h := NewHeadless(New(in), 20, 1)
	h.Type("old")
	in.SetValue("")
	h.Type("new")
	h.Press("ctrl+z")
	if in.Value() != "" {
		t.Errorf("undo left %q", in.Value())
	}
}

func TestInputClickAfterClearing(t *testing.T) {
	in := NewInput().SetPlaceholder("type")
	h := NewHeadless(New(in, WithMouse()), 5, 1)
	h.Type("a long text")
	h.Press("ctrl+u")
	h.Click(2, 0)
	if in.Value() != "" {
		t.Errorf("value = %q", in.Value())
	}
}

func TestTextAreaWheelScrollsAwayFromTheCursor(t *testing.T) {
	ta := NewTextArea()
	ta.SetValue(strings.Repeat("line\n", 20) + "end")
	h := NewHeadless(New(ta, WithMouse()), 10, 3)
	if ta.top != 18 {
		t.Fatalf("start top = %d", ta.top)
	}
	h.Wheel(1, 1, -2)
	if ta.top != 12 {
		t.Errorf("wheel up did not scroll: top %d", ta.top)
	}
	h.Type("!")
	if ta.top < 18 {
		t.Errorf("typing did not bring the cursor back: top %d", ta.top)
	}
}

func TestEditTextShowsSeveralLines(t *testing.T) {
	app := New(NewLabel("root", ""))
	h := NewHeadless(app, 80, 30)
	ta := app.EditText("Edit", "1\n2\n3\n4", func(string) {})
	h.Draw()
	wantContains(t, h, "1", "4")
	if ta.height < 10 {
		t.Errorf("editor height = %d", ta.height)
	}
}

func TestPausedPasteWaitsForItsEnd(t *testing.T) {
	in := NewInput()
	submitted := false
	in.OnSubmit = func(string) { submitted = true }
	app := New(in)
	NewHeadless(app, 20, 1)
	app.feed([]byte("\x1b[200~one\n"), true)
	app.feed(nil, false) // the escape timer fires during the pause
	app.feed([]byte("two\x1b[201~"), true)
	if submitted || in.Value() != "one two" {
		t.Errorf("submitted=%v value=%q", submitted, in.Value())
	}
}

func TestCtrlCQuitsWhileFiltering(t *testing.T) {
	app, _, _ := newListApp()
	h := NewHeadless(app, 30, 6)
	h.Press("/", "ctrl+c")
	if quit, _ := h.Quit(); !quit {
		t.Error("ctrl+c did not quit while the filter was open")
	}
}

func TestUntouchedRequiredFieldBlocksSubmit(t *testing.T) {
	in := NewInput()
	in.Validate = func(s string) error {
		if s == "" {
			return errors.New("required")
		}
		return nil
	}
	form := NewForm().Add("Name", in)
	submitted := false
	form.OnSubmit = func() { submitted = true }
	h := NewHeadless(New(form), 30, 3)
	h.Press("ctrl+s")
	if submitted {
		t.Error("submitted an empty required field")
	}
}

func TestFilterNotifiesCursorChange(t *testing.T) {
	l := NewList(func(s string) []Span { return []Span{{Text: s}} })
	l.SetItems([]string{"alpha", "beta"})
	seen := ""
	l.OnCursor = func(s string) { seen = s }
	l.SetFilter("beta")
	if seen != "beta" {
		t.Errorf("OnCursor saw %q", seen)
	}
}

func TestGrowingColumnKeepsItsMinimum(t *testing.T) {
	l := NewList(func(s string) []Span { return []Span{{Text: strings.Repeat("w", 30)}, {Text: s}} }).
		SetColumns(Column{Title: "WIDE", Min: 3}, Column{Title: "T", Grow: 1, Min: 8})
	l.SetItems([]string{"title here"})
	h := NewHeadless(New(l), 30, 3)
	if !strings.Contains(h.Lines()[1], "title h") {
		t.Errorf("row = %q", h.Lines()[1])
	}
}

func TestConfirmShowsAllOfALongQuestion(t *testing.T) {
	app := New(NewLabel("root", ""))
	h := NewHeadless(app, 120, 24)
	app.Confirm("Q", strings.Repeat("word ", 20)+"END", func() {})
	h.Draw()
	wantContains(t, h, "END")
}

func TestTextAreaUndoAfterMovingRows(t *testing.T) {
	ta := NewTextArea()
	h := NewHeadless(New(ta), 20, 4)
	h.Type("abc")
	h.Press("enter")
	h.Type("def")
	h.Press("up")
	h.Type("X")
	h.Press("ctrl+z")
	if ta.Value() != "abc\ndef" {
		t.Errorf("undo = %q", ta.Value())
	}
}

func TestSetItemsNotifiesCursor(t *testing.T) {
	l := NewList(func(s string) []Span { return []Span{{Text: s}} })
	l.SetItems([]string{"a"})
	seen := ""
	l.OnCursor = func(s string) { seen = s }
	l.SetItems([]string{"b"})
	if seen != "b" {
		t.Errorf("OnCursor saw %q", seen)
	}
}

func TestOversizedPasteDropsItsRest(t *testing.T) {
	in := NewInput()
	submitted := false
	in.OnSubmit = func(string) { submitted = true }
	app := New(in)
	NewHeadless(app, 20, 1)
	app.feed([]byte(pasteStart+strings.Repeat("x", maxPaste)), true)
	app.feed([]byte("\nmore\x1b[20"), true)
	app.feed([]byte("1~z"), true)
	if submitted || strings.Contains(in.Value(), "more") || !strings.HasSuffix(in.Value(), "xz") {
		t.Errorf("submitted=%v value ends %q", submitted, in.Value()[len(in.Value())-5:])
	}
}

func TestInputScrollsLongValueQuickly(t *testing.T) {
	in := NewInput()
	h := NewHeadless(New(in), 40, 1)
	start := time.Now()
	h.Paste(strings.Repeat("abcdefghij", 5000))
	if d := time.Since(start); d > time.Second {
		t.Errorf("drawing a long value took %v", d)
	}
	if got := h.Lines()[0]; !strings.HasSuffix(got, "j") {
		t.Errorf("view = %q", got)
	}
}

func TestMenuScrollsToTheCursor(t *testing.T) {
	app := New(NewLabel("root", ""))
	h := NewHeadless(app, 40, 6)
	var items []MenuItem
	picked := ""
	for i := range 8 {
		label := "item " + string(rune('a'+i))
		items = append(items, MenuItem{Label: label, Action: func() { picked = label }})
	}
	app.Menu("M", items)
	h.Press("end")
	h.Press("down", "down", "down", "down", "down", "down", "down")
	wantContains(t, h, "item h")
	h.Press("enter")
	if picked != "item h" {
		t.Errorf("picked %q", picked)
	}
}

func TestSuggestionWithCaseFolding(t *testing.T) {
	in := NewInput()
	in.SetSuggest(func(string) []string { return []string{"istanbul"} })
	h := NewHeadless(New(in), 20, 1)
	h.Type("I")
	h.Press("end")
	if in.Value() != "Istanbul" {
		t.Errorf("value = %q", in.Value())
	}
}

func TestTruncateLineAtASpanBoundary(t *testing.T) {
	got := TruncateLine(Line{{Text: "abc"}, {Text: "def"}}, 3).String()
	if got != "ab…" {
		t.Errorf("truncated = %q", got)
	}
}

func TestDisabledRootTakesNoKeys(t *testing.T) {
	cb := NewCheckbox("x", false).SetDisabled(true)
	h := NewHeadless(New(cb), 10, 1)
	h.Press("space")
	if cb.Value() {
		t.Error("a disabled root checkbox flipped")
	}
}

type focusWatch struct {
	*Input
	events *[]string
	name   string
}

func (f *focusWatch) FocusChanged(on bool) {
	*f.events = append(*f.events, fmt.Sprintf("%s:%v", f.name, on))
}

func TestFocusChangesAcrossLayers(t *testing.T) {
	var events []string
	root := &focusWatch{Input: NewInput(), events: &events, name: "root"}
	app := New(root)
	NewHeadless(app, 20, 4)
	events = nil
	dlg := &focusWatch{Input: NewInput(), events: &events, name: "dlg"}
	app.Push(dlg, LayerOptions{Focus: dlg})
	app.Pop()
	if got := strings.Join(events, " "); got != "root:false dlg:true dlg:false root:true" {
		t.Errorf("events = %q", got)
	}
}

func TestSetItemsNotifiesOnce(t *testing.T) {
	l := NewList(func(s string) []Span { return []Span{{Text: s}} })
	calls := 0
	l.OnCursor = func(string) { calls++ }
	l.SetItems([]string{"a", "b"})
	if calls != 1 {
		t.Errorf("OnCursor called %d times", calls)
	}
}

func TestCutPasteKeepsASplitEndMarker(t *testing.T) {
	in := NewInput()
	app := New(in)
	NewHeadless(app, 20, 1)
	app.feed([]byte(pasteStart+strings.Repeat("x", maxPaste)+"\x1b[2"), true)
	app.feed([]byte("01~q"), true)
	if !strings.HasSuffix(in.Value(), "xq") {
		t.Errorf("value ends %q", in.Value()[len(in.Value())-4:])
	}
}

func TestSearchShowsAMatchDeepInAWrappedLine(t *testing.T) {
	v := NewViewport()
	v.SetLines([]Line{Text(strings.Repeat("word ", 30)+"needle", ""), Text("tail", "")})
	h := NewHeadless(New(v), 20, 4)
	h.Press("/")
	h.Type("needle")
	h.Press("enter")
	wantContains(t, h, "needle")
}

func TestFilterPasteUndoes(t *testing.T) {
	app, l, _ := newListApp()
	h := NewHeadless(app, 30, 6)
	h.Press("/")
	h.Type("a")
	h.Paste("l")
	h.Press("ctrl+z")
	if l.filter.String() != "a" {
		t.Errorf("filter after undo = %q", l.filter.String())
	}
}

func TestSetValueKeepsSpaces(t *testing.T) {
	in := NewInput()
	in.SetValue("  a  b ")
	if in.Value() != "  a  b " {
		t.Errorf("value = %q", in.Value())
	}
}

func TestJoinedEmojiIsTypedWhole(t *testing.T) {
	in := NewInput()
	app := New(in)
	NewHeadless(app, 20, 1)
	app.feed([]byte("👩‍💻"), false)
	if in.Value() != "👩‍💻" || len(in.ed.lines[0]) != 1 {
		t.Errorf("value = %q in %d clusters", in.Value(), len(in.ed.lines[0]))
	}
}

func TestViewportSearchCursorOnlyWithFocus(t *testing.T) {
	v := NewViewport()
	b := NewButton("ok", nil)
	h := NewHeadless(New(VBox(Grow(v), Fit(b))), 20, 4)
	h.Press("/", "tab")
	if x, y := h.Cursor(); x >= 0 || y >= 0 {
		t.Errorf("cursor shown at %d,%d", x, y)
	}
}

func TestLongFilterKeepsTheCaretInView(t *testing.T) {
	app, _, _ := newListApp()
	h := NewHeadless(app, 12, 6)
	h.Press("/")
	h.Type("abcdefghijklmnop")
	x, y := h.Cursor()
	if x < 0 || y < 0 || !strings.Contains(h.Screen(), "nop") {
		t.Errorf("cursor %d,%d screen:\n%s", x, y, h.Screen())
	}
}

func TestSecretClickCountsBullets(t *testing.T) {
	in := NewInput().SetSecret(true)
	in.SetValue("界界abc")
	h := NewHeadless(New(in, WithMouse()), 20, 1)
	h.Click(3, 0)
	h.Type("X")
	if in.Value() != "界界aXbc" {
		t.Errorf("value = %q", in.Value())
	}
}

func TestClickEndsTheUndoGroup(t *testing.T) {
	in := NewInput()
	h := NewHeadless(New(in, WithMouse()), 20, 1)
	h.Type("abc")
	h.Click(0, 0)
	h.Type("X")
	h.Press("ctrl+z")
	if in.Value() != "abc" {
		t.Errorf("undo = %q", in.Value())
	}
}

func TestChoicesShowAllAfterGrowing(t *testing.T) {
	r := NewRadioGroup([]string{"a", "b", "c", "d", "e"}, 4)
	h := NewHeadless(New(r), 10, 2)
	h.Resize(10, 5)
	wantContains(t, h, "( ) a", "(•) e")
}

type valueWidget struct{ lines []string }

func (v valueWidget) Draw(c Canvas) { c.Text(0, 0, v.lines[0], "") }

func TestValueWidgetsDoNotPanic(t *testing.T) {
	h := NewHeadless(New(VBox(Grow(valueWidget{lines: []string{"hello"}}))), 20, 2)
	wantContains(t, h, "hello")
}

func TestDeletingAJoinerMergesClusters(t *testing.T) {
	in := NewInput()
	in.SetValue("🇩x🇪")
	h := NewHeadless(New(in), 20, 1)
	h.Press("left", "backspace")
	if in.Value() != "🇩🇪" || len(in.ed.lines[0]) != 1 {
		t.Fatalf("value = %q in %d clusters", in.Value(), len(in.ed.lines[0]))
	}
	h.Press("backspace")
	if in.Value() != "" {
		t.Errorf("value after deleting the flag = %q", in.Value())
	}
}

func TestLineStopsAtAClippedWideCharacter(t *testing.T) {
	b := newBuffer(3, 1)
	Canvas{f: &frame{buf: b}, w: 3, h: 1}.Line(0, 0, Line{{Text: "ab界"}, {Text: "X"}})
	if got := b.lines()[0]; got != "ab" {
		t.Errorf("line = %q", got)
	}
}

func TestSetRootBlursTheOldFocus(t *testing.T) {
	var events []string
	old := &focusWatch{Input: NewInput(), events: &events, name: "old"}
	app := New(old)
	NewHeadless(app, 10, 1)
	events = nil
	app.SetRoot(NewInput())
	if strings.Join(events, " ") != "old:false" {
		t.Errorf("events = %q", events)
	}
}

func TestScrollByAfterSetLines(t *testing.T) {
	v := NewViewport()
	h := NewHeadless(New(v), 10, 3)
	var lines []Line
	for range 20 {
		lines = append(lines, Text("x", ""))
	}
	v.SetLines(lines)
	v.ScrollBy(5)
	h.Draw()
	if v.top != 5 {
		t.Errorf("top = %d", v.top)
	}
}

func TestHorizontalChoicesScrollToTheCursor(t *testing.T) {
	cl := NewCheckList([]string{"open", "inProgress", "waiting", "answered", "closed"}).SetHorizontal(true)
	h := NewHeadless(New(cl), 30, 1)
	h.Press("end")
	wantContains(t, h, "closed")
	h.Press("home")
	wantContains(t, h, "open")
}

func TestHorizontalChoicesComeBackWhenWider(t *testing.T) {
	r := NewRadioGroup([]string{"one", "two", "three"}, 0).SetHorizontal(true)
	h := NewHeadless(New(r), 14, 1)
	h.Press("end")
	h.Resize(60, 1)
	wantContains(t, h, "one", "three")
}

func TestZeroWidthCharacterTakesNoCell(t *testing.T) {
	b := newBuffer(4, 1)
	Canvas{f: &frame{buf: b}, w: 4, h: 1}.Line(0, 0, Line{{Text: "a\u200b"}, {Text: "b"}})
	if got := b.lines()[0]; got != "ab" {
		t.Errorf("line = %q", got)
	}
}

func TestPopNotifiesAReplacementOnce(t *testing.T) {
	var events []string
	first := &focusWatch{Input: NewInput(), events: &events, name: "first"}
	second := &focusWatch{Input: NewInput(), events: &events, name: "second"}
	app := New(VBox(Fit(first), Fit(second)))
	NewHeadless(app, 20, 2)
	app.Push(NewLabel("modal", ""), LayerOptions{})
	first.SetDisabled(true)
	events = nil
	app.Pop()
	if got := strings.Join(events, " "); got != "second:true" {
		t.Errorf("events = %q", got)
	}
}

func TestReadableOnCursor(t *testing.T) {
	cursor := DefaultTheme().Cursor
	muted := theme.Join(cursor, theme.Fg256(244))
	got := readable(muted)
	if got == muted {
		t.Fatalf("muted on the cursor kept %q", got)
	}
	fg, bg, _ := colors(got)
	if r := contrast(fg.c, bg.c); r < minContrast {
		t.Errorf("contrast of %q = %.2f", got, r)
	}
	if fg.c.r != fg.c.g || fg.c.g != fg.c.b {
		t.Errorf("muted grey turned %v", fg.c)
	}
	for _, s := range []theme.Style{
		theme.Join(cursor, theme.Fg256(203)),      // Readable already.
		theme.Join(cursor, theme.Fg256(244), "7"), // Reverse video.
		theme.Join(cursor, "31"),                  // The terminal's own red.
		theme.Fg256(244),                          // No background.
	} {
		if got := readable(s); got != s {
			t.Errorf("readable(%q) = %q", s, got)
		}
	}
	if got := readable(theme.Join(muted, "58;2;255;0;0;4")); got == theme.Join(muted, "58;2;255;0;0;4") {
		t.Errorf("an underline color stopped the shade: %q", got)
	}
	red := readable(theme.Join(theme.Bg256(238), theme.Fg256(88)))
	if fg, _, _ := colors(red); fg.c.r <= fg.c.g || fg.c.r <= fg.c.b {
		t.Errorf("dark red turned %v", fg.c)
	}
	light := readable(theme.Join(theme.Bg256(254), theme.Fg256(229)))
	if fg, bg, _ := colors(light); luminance(fg.c) >= luminance(bg.c) || contrast(fg.c, bg.c) < minContrast {
		t.Errorf("pale yellow on white = %q", light)
	}
}

func TestCursorKeepsRowColor(t *testing.T) {
	l := NewList(func(s string) []Span { return []Span{{Text: s}} })
	l.SetItems([]string{"a", "b"})
	l.SetRowStyle(func(string) theme.Style { return theme.Join(theme.Fg256(203), theme.Bg256(52)) })
	h := NewHeadless(New(l), 10, 3)
	var cell cell
	for x := range 10 {
		if c := *h.buf.at(x, 0); c.text == "a" {
			cell = c
		}
	}
	fg, bg, reverse := colors(cell.style)
	_, want, _ := colors(DefaultTheme().Cursor)
	if fg.c != palette256(203) || bg != want || reverse {
		t.Errorf("cursor over a red row = %q", cell.style)
	}
}

func TestBackground(t *testing.T) {
	for in, want := range map[theme.Style]theme.Style{
		"48;5;238;38;5;255":       "48;5;238",
		"38;2;1;2;3;48;2;5;6;7;1": "48;2;5;6;7",
		"1;41;38;5;48":            "41",
		"48;2;1;2;3;0;1":          "",
		"104;49;45":               "45",
		"38;5;4":                  "",
		"58;5;44;4":               "",
	} {
		if got := background(in); got != want {
			t.Errorf("background(%q) = %q, want %q", in, got, want)
		}
	}
}
