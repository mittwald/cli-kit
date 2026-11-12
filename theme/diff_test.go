package theme

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mittwald/cli-kit/term"
)

// classify runs a scanner over the lines of diff and returns what it told
// each line, by its first letter: t text, f file header, h hunk header,
// c context, - removed, + added.
func classify(diff string) string {
	letters := map[DiffKind]byte{DiffText: 't', DiffFileHeader: 'f', DiffHunkHeader: 'h', DiffContext: 'c', DiffRemoved: '-', DiffAdded: '+'}
	var s DiffScanner
	var b strings.Builder
	for line := range strings.SplitSeq(diff, "\n") {
		b.WriteByte(letters[s.Classify(line)])
	}
	return b.String()
}

func TestDiffScanner(t *testing.T) {
	tests := map[string]struct{ diff, want string }{
		"git diff": {
			"commit message\n" +
				"diff --git a/x b/x\n" +
				"index 1234567..89abcde 100644\n" +
				"--- a/x\n" +
				"+++ b/x\n" +
				"@@ -1,3 +1,3 @@ func main() {\n" +
				" context\n" +
				"-old\n" +
				"+new\n" +
				" context",
			"tffffhc-+c",
		},
		// A removed line that reads like a file header is still removed while
		// the hunk has old lines to come, and an added one likewise.
		"header-like changes": {
			"--- a/x\n+++ b/x\n@@ -1,2 +1,2 @@\n--- a/old\n-- b\n+++ new\n++ b",
			"ffh--++",
		},
		// Once the counts are through, the next file starts.
		"two files": {
			"--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n--- a/y\n+++ b/y\n@@ -0,0 +1 @@\n+c",
			"ffh-+ffh+",
		},
		"two hunks": {
			"@@ -1,2 +1,2 @@\n-a\n+b\n c\n@@ -10 +10 @@\n-d\n+e",
			"h-+ch-+",
		},
		"no newline note": {
			"@@ -1 +1 @@\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file",
			"h-c+c",
		},
		"blank context line": {
			"@@ -1,3 +1,3 @@\n a\n\n-b\n+c",
			"hcc-+",
		},
		"mode and rename lines": {
			"diff --git a/x b/y\nsimilarity index 90%\nrename from x\nrename to y\nnew mode 100755\nBinary files a/x and b/y differ\ndiff --git a/z b/z",
			"fffffff",
		},
		// GitLab's API gives a file's hunks alone.
		"hunks alone": {
			"@@ -3,4 +3,4 @@\n a\n-b\n+c\n d\n e",
			"hc-+cc",
		},
		"text after the hunk": {
			"@@ -1 +1 @@\n-a\n+b\n-- \n2.47.0",
			"h-+tt",
		},
		"uncounted hunk": {
			"@@ unknown @@\n-a\n+b\n c\n--- a/next\n@@ -1 +1 @@\n-x",
			"h-+c-h-",
		},
		"empty hunk": {
			"@@ -0,0 +0,0 @@\n-a",
			"ht",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := classify(tt.diff); got != tt.want {
				t.Errorf("classified %q as %s, want %s", tt.diff, got, tt.want)
			}
		})
	}
}

func TestPaintDiffLine(t *testing.T) {
	th := Theme{Added: "32", Removed: "31", DiffHeader: "1", DiffHunk: "36"}
	tests := map[string]struct {
		kind       DiffKind
		line, want string
	}{
		"header":            {DiffFileHeader, "--- a/x", "\x1b[1m--- a/x\x1b[0m"},
		"hunk":              {DiffHunkHeader, "@@ -1 +1 @@", "\x1b[36m@@ -1 +1 @@\x1b[0m"},
		"hunk and function": {DiffHunkHeader, "@@ -1,2 +1,3 @@ func main() {", "\x1b[36m@@ -1,2 +1,3 @@\x1b[0m func main() {"},
		"hunk unclosed":     {DiffHunkHeader, "@@ -1", "\x1b[36m@@ -1\x1b[0m"},
		"hunk bare":         {DiffHunkHeader, "@@", "\x1b[36m@@\x1b[0m"},
		"removed":           {DiffRemoved, "-old", "\x1b[31m-old\x1b[0m"},
		"added":             {DiffAdded, "+new", "\x1b[32m+new\x1b[0m"},
		"context":           {DiffContext, " same", " same"},
		"text":              {DiffText, "message", "message"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := th.PaintDiffLine(tt.kind, tt.line); got != tt.want {
				t.Errorf("PaintDiffLine(%d, %q) = %q, want %q", tt.kind, tt.line, got, tt.want)
			}
		})
	}
}

func TestPaintDiff(t *testing.T) {
	th := Theme{Added: "32", Removed: "31", DiffHeader: "1", DiffHunk: "36"}
	diff := "--- a/x\r\n+++ b/x\n@@ -1,2 +1,2 @@\n--- old\n+new\n context\n"
	want := "\x1b[1m--- a/x\x1b[0m\r\n" +
		"\x1b[1m+++ b/x\x1b[0m\n" +
		"\x1b[36m@@ -1,2 +1,2 @@\x1b[0m\n" +
		"\x1b[31m--- old\x1b[0m\n" +
		"\x1b[32m+new\x1b[0m\n" +
		" context\n"
	if got := th.PaintDiff(diff); got != want {
		t.Errorf("PaintDiff =\n%q\nwant\n%q", got, want)
	}
	if got := Plain.PaintDiff(diff); got != diff {
		t.Errorf("Plain.PaintDiff = %q, want the diff unchanged", got)
	}
	if got := th.PaintDiff("@@ -0,0 +1 @@\n+added"); got != "\x1b[36m@@ -0,0 +1 @@\x1b[0m\n\x1b[32m+added\x1b[0m" {
		t.Errorf("PaintDiff without a final newline = %q", got)
	}
	// Outside a hunk a line is not a change, whatever it starts with.
	if got := th.PaintDiff("+1 for this\n- a list item"); got != "+1 for this\n- a list item" {
		t.Errorf("PaintDiff of text = %q, want it unchanged", got)
	}
}

// The default diff roles are the 256-color tones of the messages, not the
// 16-color red and green whose shade every terminal picks for itself.
func TestDefaultDiffRoles(t *testing.T) {
	if Default.Added != Default.Success || Default.Removed != Default.Error {
		t.Errorf("Default.Added, Removed = %q, %q, want Success's %q and Error's %q", Default.Added, Default.Removed, Default.Success, Default.Error)
	}
	if Default.DiffHeader == "" || Default.DiffHunk == "" {
		t.Errorf("Default.DiffHeader, DiffHunk = %q, %q, want both set", Default.DiffHeader, Default.DiffHunk)
	}
}

// For keeps a theme only for a writer that takes color.
func TestThemeFor(t *testing.T) {
	was := term.CurrentColorMode()
	t.Cleanup(func() { term.SetColorMode(was) })
	var out bytes.Buffer

	term.SetColorMode(term.ColorAuto)
	if got := Default.For(&out); got != Plain {
		t.Errorf("For(a buffer) = %+v, want Plain: a buffer is no terminal", got)
	}
	term.SetColorMode(term.ColorAlways)
	if got := Default.For(&out); got != Default {
		t.Errorf("For under --color=always = %+v, want Default", got)
	}
	term.SetColorMode(term.ColorNever)
	if got := Default.For(&out); got != Plain {
		t.Errorf("For under --color=never = %+v, want Plain", got)
	}
}
