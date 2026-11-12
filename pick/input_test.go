package pick

import (
	"errors"
	"io"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mittwald/cli-kit/term"
)

func TestEditorEditsTheValue(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		keys  []key
		start string
		want  string
	}{
		{name: "accepts the value it started with", start: "-rc", want: "-rc"},
		{name: "backspace takes the rune before the caret", start: "-rc", keys: []key{{kind: keyBackspace}}, want: "-r"},
		{name: "typing inserts at the caret", start: "-r", keys: []key{{kind: keyRune, rune: 'c'}}, want: "-rc"},
		{
			name:  "the caret moves and inserts where it stands",
			start: "-rc1",
			keys:  []key{{kind: keyLeft}, {kind: keyLeft}, {kind: keyRune, rune: 'x'}},
			want:  "-rxc1",
		},
		{name: "Home inserts in front", start: "rc", keys: []key{{kind: keyHome}, {kind: keyRune, rune: '-'}}, want: "-rc"},
		{name: "Delete takes the rune under the caret", start: "-rc", keys: []key{{kind: keyHome}, {kind: keyDelete}}, want: "rc"},
		{
			name:  "Ctrl-K kills what follows the caret",
			start: "-rc1",
			keys:  []key{{kind: keyLeft}, {kind: keyLeft}, {kind: keyKillToEnd}},
			want:  "-r",
		},
		{
			// Punctuation holds a value together: the whole word goes.
			name:  "Ctrl-W takes the word before the caret",
			start: "v1.2-rc1",
			keys:  []key{{kind: keyDeleteWord}},
			want:  "",
		},
		{
			name:  "Ctrl-W stops at whitespace",
			start: "two words",
			keys:  []key{{kind: keyDeleteWord}},
			want:  "two ",
		},
		{name: "Ctrl-U deletes back to the start", start: "-rc", keys: []key{{kind: keyClearFilter}}, want: ""},
		{name: "Ctrl-U keeps what follows the caret", start: "-rc", keys: []key{{kind: keyLeft}, {kind: keyClearFilter}}, want: "c"},
		{name: "backspace on an empty value does nothing", keys: []key{{kind: keyBackspace}, {kind: keyBackspace}}},
		{
			name:  "the caret stops at the ends",
			start: "ab",
			keys:  []key{{kind: keyRight}, {kind: keyRight}, {kind: keyRight}, {kind: keyRune, rune: 'c'}},
			want:  "abc",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			e := newEditor(InputOptions{Prompt: "Suffix: ", Value: testCase.start}, false)
			for _, k := range testCase.keys {
				if got := e.apply(k); got != actionNone {
					t.Fatalf("apply(%v) = %v, want actionNone", k, got)
				}
			}
			if got := e.answer(); got != testCase.want {
				t.Errorf("answer() = %q, want %q", got, testCase.want)
			}
			if got := e.apply(key{kind: keyEnter}); got != actionSelect {
				t.Errorf("apply(Enter) = %v, want actionSelect", got)
			}
		})
	}
}

func TestEditorRendersOnePlainLine(t *testing.T) {
	e := newEditor(InputOptions{Prompt: "Suffix for %s: ", Subject: "tool 1.2.3", Value: "-rc"}, false)

	got := e.render(80)
	want := []string{"Suffix for tool 1.2.3: -rc"}
	if !slices.Equal(got, want) {
		t.Fatalf("render() = %q, want %q", got, want)
	}
	if got, want := e.cursorColumn(80), len(want[0])+1; got != want {
		t.Errorf("cursorColumn() = %d, want %d", got, want)
	}
	e.apply(key{kind: keyHome})
	if got, want := e.cursorColumn(80), len("Suffix for tool 1.2.3: ")+1; got != want {
		t.Errorf("cursorColumn() at the start = %d, want %d", got, want)
	}
}

// The subject and the value are styled; the prompt around them stays plain.
func TestEditorColoredRender(t *testing.T) {
	e := newEditor(InputOptions{Prompt: "Suffix for %s: ", Subject: "tool 1.2.3", Value: "-rc"}, true)
	want := "Suffix for " + th.Subject.Paint("tool 1.2.3") + ": " + th.Value.Paint("-rc")
	if got := e.render(80)[0]; got != want {
		t.Errorf("render() = %q, want %q", got, want)
	}
	if got, want := e.cursorColumn(80), len("Suffix for tool 1.2.3: -rc")+1; got != want {
		t.Errorf("cursorColumn() = %d, want %d", got, want)
	}
}

// The answers a question spells out recede.
func TestEditorRendersTheChoicesReceding(t *testing.T) {
	e := newEditor(InputOptions{Prompt: "Recreate %s? [y/N]: ", Subject: "tool-1.2", Value: "y"}, true)
	want := "Recreate " + th.Subject.Paint("tool-1.2") + "? " + th.Qualifier.Paint("[y/N]") + ": " + th.Value.Paint("y")
	if got := e.render(80)[0]; got != want {
		t.Errorf("render() = %q, want %q", got, want)
	}
	e = newEditor(InputOptions{Prompt: "Apply now? [y/N]: "}, true)
	want = "Apply now? " + th.Qualifier.Paint("[y/N]") + ": "
	if got := e.render(80)[0]; got != want {
		t.Errorf("render() = %q, want %q", got, want)
	}
}

// A line cut to the terminal keeps the cursor on screen.
func TestEditorCursorStaysOnTheLine(t *testing.T) {
	e := newEditor(InputOptions{Prompt: "Suffix: ", Value: "-a-rather-long-variant-name"}, false)
	if got := e.cursorColumn(12); got != 12 {
		t.Errorf("cursorColumn() = %d, want it clamped to 12", got)
	}
	for _, line := range e.render(12) {
		if width(line) > 12 {
			t.Errorf("render() = %q, wider than the terminal", line)
		}
	}
}

// A secret field draws nothing for its value, and the cursor stays where the
// prompt ends.
func TestEditorKeepsASecretOffTheScreen(t *testing.T) {
	e := newEditor(InputOptions{Prompt: "Password: ", Secret: true}, true)
	typeText(e, "hunter2")

	line := e.render(80)[0]
	if got, want := line, "Password: "; got != want {
		t.Errorf("render() = %q, want %q", got, want)
	}
	if got, want := e.cursorColumn(80), len("Password: ")+1; got != want {
		t.Errorf("cursorColumn() = %d, want %d", got, want)
	}
	e.apply(key{kind: keyBackspace})
	if got, want := e.answer(), "hunter"; got != want {
		t.Errorf("answer() = %q, want %q", got, want)
	}
}

func TestDriveEdits(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		chunks [][]byte
		want   string
		err    error
	}{
		{name: "Enter accepts the value", chunks: [][]byte{[]byte("\r")}, want: "-rc"},
		{name: "a correction is typed over it", chunks: [][]byte{[]byte("\x7f\x7fbeta\r")}, want: "-beta"},
		{name: "Esc goes back", chunks: [][]byte{{0x1b}}, err: ErrBack},
		{name: "Ctrl-C interrupts", chunks: [][]byte{{0x03}}, err: ErrInterrupted},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			e := newEditor(InputOptions{Prompt: "Suffix: ", Value: "-rc"}, false)
			err := drive(e, &renderer{w: io.Discard}, fixedSize(80, 24), 0, &scriptedInput{chunks: testCase.chunks})
			if !errors.Is(err, testCase.err) {
				t.Fatalf("drive() error = %v, want %v", err, testCase.err)
			}
			if err == nil && e.answer() != testCase.want {
				t.Errorf("answer() = %q, want %q", e.answer(), testCase.want)
			}
		})
	}
}

// The terminal's own cursor is put where the caret is after every redraw; a
// block that draws its own cursor gets none.
func TestDrivePlacesTheTerminalCursor(t *testing.T) {
	var out strings.Builder
	e := newEditor(InputOptions{Prompt: "Suffix: ", Value: "-rc1"}, false)
	_ = drive(e, &renderer{w: &out}, fixedSize(80, 24), 0, &scriptedInput{chunks: [][]byte{[]byte("\r")}})
	if want := "\r\x1b[13G"; !strings.Contains(out.String(), want) {
		t.Errorf("drive() wrote %q, want it to place the cursor with %q", out.String(), want)
	}

	var listOut strings.Builder
	l := testList(t, 3, []string{"1.4.24"})
	_ = drive(l, &renderer{w: &listOut}, fixedSize(80, 24), 0, &scriptedInput{chunks: [][]byte{[]byte("\r")}})
	if strings.Contains(listOut.String(), "G") {
		t.Errorf("drive() placed a cursor for a list: %q", listOut.String())
	}
}

// The prompt, its subject and the value a field starts with are drawn as
// plain text, and the value comes back the way it was shown.
func TestEditorDrawsCallerTextPlain(t *testing.T) {
	e := newEditor(InputOptions{Prompt: "Name\x1b[2J for %s:\t", Subject: "x\x1b]0;t\x07y\n", Value: "v\u009b2Jw\rz"}, false)
	if got, want := e.render(80)[0], "Name for xy : vw z"; got != want {
		t.Errorf("render() = %q, want %q", got, want)
	}
	if got, want := e.answer(), "vw z"; got != want {
		t.Errorf("answer() = %q, want %q", got, want)
	}
	// The space a prompt ends in is the caller's and stays.
	e = newEditor(InputOptions{Prompt: "Password: ", Secret: true}, false)
	if got, want := e.render(80)[0], "Password: "; got != want {
		t.Errorf("render() = %q, want %q", got, want)
	}
}

// The caret is drawn where the prompt as drawn ends: the subject goes in
// for the first %s only, in the line and in the count alike.
func TestEditorCountsThePromptAsItIsDrawn(t *testing.T) {
	e := newEditor(InputOptions{Prompt: "Rename %s (was %s): ", Subject: "production", Value: "x"}, false)
	line := e.render(80)[0]
	if want := "Rename production (was %s): x"; line != want {
		t.Fatalf("render() = %q, want %q", line, want)
	}
	if got, want := e.cursorColumn(80), width(line)+1; got != want {
		t.Errorf("cursorColumn() = %d, want %d, right behind the value", got, want)
	}
}

// What is drawn as one character is one step for the caret and one
// Backspace, however many code points it is made of.
func TestEditorStepsOverGraphemeClusters(t *testing.T) {
	const family = "👨\u200d👩\u200d👧"
	e := newEditor(InputOptions{Prompt: "> ", Value: "a" + family + "b"}, false)
	end := e.cursorColumn(80)
	e.apply(key{kind: keyLeft})
	e.apply(key{kind: keyLeft})
	if got, want := e.cursorColumn(80), end-1-width(family); got != want {
		t.Errorf("two steps left: column %d, want %d, in front of the emoji", got, want)
	}
	e.apply(key{kind: keyRight})
	if got, want := e.cursorColumn(80), end-1; got != want {
		t.Errorf("a step right: column %d, want %d, behind the emoji", got, want)
	}
	e.apply(key{kind: keyBackspace})
	if got, want := e.answer(), "ab"; got != want {
		t.Errorf("Backspace behind the emoji left %+q, want %q", got, want)
	}
	e.apply(key{kind: keyHome})
	e.apply(key{kind: keyRight})

	for _, c := range []struct {
		name, value string
		keys        []key
		want        string
	}{
		{"Backspace takes a flag whole", "x🇩🇪", []key{{kind: keyBackspace}}, "x"},
		{"Backspace takes a letter with its accent", "cafe\u0301", []key{{kind: keyBackspace}}, "caf"},
		{"Delete takes a letter with its accent", "e\u0301x", []key{{kind: keyHome}, {kind: keyDelete}}, "x"},
		{"typing in front of an accented letter keeps the accent on it", "cafe\u0301", []key{{kind: keyLeft}, {kind: keyRune, rune: 'X'}}, "cafXe\u0301"},
		{"an accent typed joins the letter before it", "cafe", []key{{kind: keyRune, rune: '\u0301'}, {kind: keyLeft}, {kind: keyRune, rune: '-'}}, "caf-e\u0301"},
		{"a joiner typed between two emoji makes them one", "👨👩", []key{{kind: keyLeft}, {kind: keyRune, rune: '\u200d'}, {kind: keyBackspace}}, ""},
	} {
		e := newEditor(InputOptions{Value: c.value}, false)
		for _, k := range c.keys {
			e.apply(k)
		}
		if got := e.answer(); got != c.want {
			t.Errorf("%s: answer() = %+q, want %+q", c.name, got, c.want)
		}
	}
}

// A value longer than the line scrolls with the caret, so what is typed is
// always on screen, with an ellipsis where text is out of sight.
func TestEditorScrollsALongValue(t *testing.T) {
	e := newEditor(InputOptions{Prompt: "URL: ", Value: "https://example.com/a/very/long/path"}, false)
	check := func(step, wantLine string, wantColumn int) {
		t.Helper()
		line := e.render(20)[0]
		if line != wantLine {
			t.Errorf("%s: render() = %q, want %q", step, line, wantLine)
		}
		if width(line) > 20 {
			t.Errorf("%s: the line is %d cells wide on 20", step, width(line))
		}
		if got := e.cursorColumn(20); got != wantColumn {
			t.Errorf("%s: cursorColumn() = %d, want %d", step, got, wantColumn)
		}
	}
	check("opened", "URL: …ery/long/path", 20)
	typeText(e, "XYZ")
	check("typed at the end", "URL: …/long/pathXYZ", 20)
	for range 5 {
		e.apply(key{kind: keyLeft})
	}
	check("moved inside the window", "URL: …/long/pathXYZ", 15)
	e.apply(key{kind: keyHome})
	check("at the start", "URL: https://exampl…", 6)
	for range 16 {
		e.apply(key{kind: keyRight})
	}
	check("scrolled right", "URL: …ps://example.…", 20)
	e.apply(key{kind: keyEnd})
	for range 30 {
		e.apply(key{kind: keyBackspace})
	}
	check("fits again", "URL: https://e", 15)

	// Wide characters scroll by whole characters and never split.
	e = newEditor(InputOptions{Prompt: "名前: ", Value: "日本語のテキストを入力する"}, false)
	for n := 1; n <= 40; n++ {
		for _, k := range []keyKind{keyEnd, keyLeft, keyLeft, keyHome, keyRight} {
			e.apply(key{kind: k})
			line := e.render(n)[0]
			if width(line) > n {
				t.Fatalf("width %d: %q is %d cells wide", n, line, width(line))
			}
			if column := e.cursorColumn(n); column < 1 || column > n {
				t.Fatalf("width %d: cursor column %d", n, column)
			}
		}
	}

	// A prompt that fills the line makes room for the field.
	e = newEditor(InputOptions{Prompt: "A rather long question: ", Value: "answer"}, false)
	check("long prompt", "A rather lon…answer", 20)
}

// The clusters around a caret are found from a place nearby. Every answer
// has to be the one a reading of the whole value gives, at every position
// of values that mix what joins runes into clusters with what never does.
func TestClusterLookupAgreesWithTheWholeValue(t *testing.T) {
	pieces := []string{
		"a", "b", " ", "-", "é", "e\u0301", "\u0301", "界", "한", "\u1100\u1161\u11a8",
		"👨\u200d👩\u200d👧", "\u200d", "👍🏽", "🏽", "🇩🇪", "\U0001f1e9", "1\ufe0f\u20e3", "\ufe0f",
		strings.Repeat("\u0301", 40), strings.Repeat("界", 70), strings.Repeat("ab", 20),
	}
	random := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // a fixed seed keeps the test repeatable; nothing here is secret
	for range 1000 {
		var b strings.Builder
		for range 1 + random.IntN(12) {
			b.WriteString(pieces[random.IntN(len(pieces))])
		}
		value := []rune(b.String())
		bounds := clusterBounds(value)
		for caret := 0; caret <= len(value); caret++ {
			before, after, end := 0, len(value), len(value)
			for _, bound := range bounds {
				if bound < caret {
					before = bound
				}
			}
			for _, bound := range slices.Backward(bounds) {
				if bound > caret {
					after = bound
				}
				if bound >= caret {
					end = bound
				}
			}
			if got := clusterBefore(value, caret); got != before {
				t.Fatalf("clusterBefore(%q, %d) = %d, want %d", string(value), caret, got, before)
			}
			if got := clusterAfter(value, caret); got != after {
				t.Fatalf("clusterAfter(%q, %d) = %d, want %d", string(value), caret, got, after)
			}
			if got := clusterEnd(value, caret); got != end {
				t.Fatalf("clusterEnd(%q, %d) = %d, want %d", string(value), caret, got, end)
			}
		}
	}
}

// A pasted value arrives rune by rune. Reading the whole value for each of
// them took seconds for a few thousand runes and left the prompt dead
// meanwhile; the time a paste takes has to grow with its length, not with
// the square of it.
func TestEditorTakesALongPaste(t *testing.T) {
	const runes, edits = 40000, 4000
	paste := func(e *editor, n int) {
		for i := range n {
			e.apply(key{kind: keyRune, rune: rune('a' + i%26)})
		}
	}
	start := time.Now()

	e := newEditor(InputOptions{Prompt: "Token: "}, false)
	paste(e, runes)
	if got := len(e.value); got != runes {
		t.Fatalf("value holds %d runes, want %d", got, runes)
	}
	// In front of what is there, where every rune has text behind it.
	e.apply(key{kind: keyHome})
	paste(e, edits)
	// And taken out again from the end, one character at a time.
	e.apply(key{kind: keyEnd})
	for range edits {
		e.apply(key{kind: keyBackspace})
	}
	if got := len(e.value); got != runes {
		t.Fatalf("value holds %d runes after the deletions, want %d", got, runes)
	}

	// The first paste alone took more than a minute when every key read
	// the whole value.
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Errorf("pasting %d runes and editing %d took %v", runes, edits, elapsed)
	}
}

// A value far longer than the line is laid out in a time that grows with
// its length. Scrolling to the caret summed the width of what was left at
// every cluster it passed, which took seconds for a long value and froze
// the prompt on its first draw.
func TestEditorLaysOutALongValue(t *testing.T) {
	const runes = 400000
	e := newEditor(InputOptions{Prompt: "Token: ", Value: strings.Repeat("x", runes)}, false)
	start := time.Now()

	// The caret is at the end: the line shows the end of the value.
	parts, column := e.layout(80)
	if shown := parts[len(parts)-1].text; shown != term.Ellipsis+strings.Repeat("x", 71) {
		t.Errorf("value scrolled to its end is drawn as %q", shown)
	}
	if column != 80 {
		t.Errorf("cursor column = %d, want 80", column)
	}
	// And back at the start, its beginning.
	e.apply(key{kind: keyHome})
	parts, column = e.layout(80)
	if shown := parts[len(parts)-1].text; shown != strings.Repeat("x", 72)+term.Ellipsis {
		t.Errorf("value scrolled to its start is drawn as %q", shown)
	}
	if column != 8 {
		t.Errorf("cursor column = %d, want 8", column)
	}
	e.apply(key{kind: keyEnd})
	e.layout(80)

	// The first layout alone took most of a minute.
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Errorf("laying out %d runes three times took %v", runes, elapsed)
	}
}
