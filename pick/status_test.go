package pick

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// A status line is taken back by moving over it, so the escape names the
// exact number of lines written, and nothing at all when there are none.
func TestClearLines(t *testing.T) {
	for _, testCase := range []struct {
		lines int
		want  string
	}{
		{lines: 1, want: "\r\x1b[1A\x1b[J"},
		{lines: 2, want: "\r\x1b[2A\x1b[J"},
		{lines: 0, want: ""},
		{lines: -1, want: ""},
	} {
		var out bytes.Buffer
		clearLines(&out, testCase.lines)
		if got := out.String(); got != testCase.want {
			t.Errorf("clearLines(%d) = %q, want %q", testCase.lines, got, testCase.want)
		}
	}
}

// Off a terminal the line stays, without escapes: a log is read after the
// fact.
func TestStatusKeepsTheLineOffATerminal(t *testing.T) {
	var out bytes.Buffer
	done := Status(&out, "Fetching releases...")
	done()
	if got, want := out.String(), "Fetching releases...\n"; got != want {
		t.Errorf("Status() wrote %q, want %q", got, want)
	}
}

// Where color is on, a status line is drawn in the theme's Status style.
func TestStatusStyle(t *testing.T) {
	was := term.CurrentColorMode()
	term.SetColorMode(term.ColorAlways)
	t.Cleanup(func() { term.SetColorMode(was); theme.Use(theme.Default) })

	var out bytes.Buffer
	Status(&out, "Fetching releases...\nand tags...")
	want := theme.Default.Status.PaintLines("Fetching releases...\nand tags...") + "\n"
	if got := out.String(); got != want {
		t.Errorf("Status() with the default theme wrote %q, want %q", got, want)
	}

	custom := theme.Default
	custom.Status = theme.Fg256(248)
	theme.Use(custom)
	out.Reset()
	Status(&out, "Fetching releases...")
	if got, want := out.String(), "\x1b[38;5;248mFetching releases...\x1b[0m\n"; got != want {
		t.Errorf("Status() with a Status style wrote %q, want %q", got, want)
	}
}

// A name reads the same reported as on the row it was chosen from.
func TestName(t *testing.T) {
	got := name("Legacy Backend (v1/archive)", th)
	want := th.Subject.Paint("Legacy Backend ") + th.Qualifier.Paint("(v1/archive)")
	if got != want {
		t.Errorf("name() = %q, want %q", got, want)
	}
	if got, want := name("tool-1.2.3", th), th.Subject.Paint("tool-1.2.3"); got != want {
		t.Errorf("name() = %q, want %q", got, want)
	}

	var out bytes.Buffer
	if got := Name(&out, "tool-1.2.3"); strings.Contains(got, "\x1b") {
		t.Errorf("Name() off a terminal = %q, want it plain", got)
	}
}

// Off a terminal an answered prompt is the question with its subject filled
// in.
func TestPrompt(t *testing.T) {
	var out bytes.Buffer
	if got, want := Prompt(&out, "Recreate %s? [y/N]: ", "tool-1.2"), "Recreate tool-1.2? [y/N]: "; got != want {
		t.Errorf("Prompt() off a terminal = %q, want %q", got, want)
	}
}

// A status line names things from elsewhere. What it writes is plain text,
// on a terminal and off it.
func TestStatusWritesPlainText(t *testing.T) {
	var out bytes.Buffer
	Status(&out, "Fetching \x1b]52;c;cm0=\x07\x1b[2Jreleases\r\nof a\tb...")
	if got, want := out.String(), "Fetching releases\nof a    b...\n"; got != want {
		t.Errorf("Status() wrote %q, want %q", got, want)
	}
}

// A value and a question written beside the prompts read the way the
// prompts draw them: as plain text, the subject filled in once.
func TestNameAndPromptArePlain(t *testing.T) {
	var out bytes.Buffer
	if got, want := Name(&out, "tool\x1b[2J-1.2\n(v1)"), "tool-1.2 (v1)"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if got, want := Prompt(&out, "Rename %s\x1b[2J (was %s): ", "pro\tduction"), "Rename pro duction (was %s): "; got != want {
		t.Errorf("Prompt() = %q, want %q", got, want)
	}

	was := term.CurrentColorMode()
	term.SetColorMode(term.ColorAlways)
	t.Cleanup(func() { term.SetColorMode(was) })
	if got, want := term.StripANSI(Prompt(&out, "Rename %s (was %s): ", "production")), "Rename production (was %s): "; got != want {
		t.Errorf("Prompt() in color reads %q, want %q, as without", got, want)
	}
}
