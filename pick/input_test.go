package pick

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
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
