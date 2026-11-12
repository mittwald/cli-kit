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
		// A renamed file has no hunk: its header ends with its last header
		// line, and the next commit of a git log -p is text again.
		"text after a file without a hunk": {
			"commit 1111\n\n    first\n\n" +
				"diff --git a/x b/y\nsimilarity index 100%\nrename from x\nrename to y\n" +
				"\ncommit 2222\nAuthor: B\n\n    second\n\n" +
				"diff --git a/z b/z\nindex 1..2 100644\n--- a/z\n+++ b/z\n@@ -1 +1 @@\n-a\n+b",
			"tttt" + "ffff" + "tttttt" + "ffffh-+",
		},
		"text after a binary file": {
			"diff --git a/i.png b/i.png\nindex 1..2 100644\nBinary files a/i.png and b/i.png differ\n\ncommit 2222\n    message",
			"fffttt",
		},
		"binary patch": {
			"diff --git a/i.png b/i.png\nindex 1..2 100644\nGIT binary patch\nliteral 5\nMcmZQzU|?imU}azc0Sf>D\n\ndiff --git a/z b/z",
			"fffttt" + "f",
		},
		"new, deleted and copied files": {
			"diff --git a/x b/x\nnew file mode 100644\nindex 0..1\n--- /dev/null\n+++ b/x\n" +
				"diff --git a/y b/y\ndeleted file mode 100644\nold mode 100644\nnew mode 100755\n" +
				"dissimilarity index 80%\ncopy from y\ncopy to z",
			"fffff" + "fffffff",
		},
		// Prose that starts with the word diff starts no file.
		"prose starting with diff": {
			"diff algorithm changed, see below\nmore prose\n- a list item\n---\n a | 2 +-\n\ndiff -u a/a b/a\n--- a/a\n+++ b/a",
			"tttttt" + "fff",
		},
		// The combined diff of a merge: one marker column per parent.
		"combined diff": {
			"diff --cc file\nindex 1,2..3\nmode 100644,100644..100755\n--- a/file\n+++ b/file\n" +
				"@@@ -1,4 -1,4 +1,5 @@@ func\n  ctx\n- old1\n -old2\n++new\n +new2\n+ new3\n  ctx\ncommit 2222",
			"fffff" + "hc--+++c" + "t",
		},
		"combined diff of three parents": {
			"@@@@ -1,2 -1,3 -1,3 +1,3 @@@@\n---a\n+++b\n   \n+  c\n+  d",
			"h-+c+t",
		},
		"combined diff, removed in one parent": {
			"@@@ -1,2 -1 +1 @@@\n- a\n  b\n--- a/next",
			"h-cf",
		},
		"combined diff without counts": {
			"@@@ unknown @@@\n -a\n+ b\n\n  c\nx",
			"h-+cct",
		},
		"combined diff, blank context line": {
			"@@@ -1,2 -1,2 +1,2 @@@\n\n++a\n--b",
			"hc+-",
		},
		"more parents than are counted": {
			"@@@@@@@@@@ -1 -1 -1 -1 -1 -1 -1 -1 -1 +1 @@@@@@@@@@\n---------a\n+++++++++b\n+++ b/x",
			"h-+f",
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

// The header of a hunk is painted up to the @ signs that close it, as many
// as open it.
func TestPaintDiffLineHunkHeaders(t *testing.T) {
	th := Theme{DiffHunk: "36"}
	tests := map[string]string{
		"@@ -1,2 +1,2 @@":             "\x1b[36m@@ -1,2 +1,2 @@\x1b[0m",
		"@@ -1,2 +1,2 @@ func main()": "\x1b[36m@@ -1,2 +1,2 @@\x1b[0m func main()",
		"@@@ -1,3 -1,3 +1,4 @@@ func": "\x1b[36m@@@ -1,3 -1,3 +1,4 @@@\x1b[0m func",
		"@@@ -1,3 -1,3 +1,4 @@@":      "\x1b[36m@@@ -1,3 -1,3 +1,4 @@@\x1b[0m",
		"@@ -1 +1 @@ a @@ b":          "\x1b[36m@@ -1 +1 @@\x1b[0m a @@ b",
		"@@ unclosed":                 "\x1b[36m@@ unclosed\x1b[0m",
		"@@":                          "\x1b[36m@@\x1b[0m",
	}
	for line, want := range tests {
		if got := th.PaintDiffLine(DiffHunkHeader, line); got != want {
			t.Errorf("PaintDiffLine(%q) = %q, want %q", line, got, want)
		}
	}
}

// A combined diff is painted like git paints it: a line is removed or added
// whichever of its columns says so.
func TestPaintDiffCombined(t *testing.T) {
	th := Theme{Added: "32", Removed: "31", DiffHeader: "1", DiffHunk: "36"}
	diff := "diff --cc file\n@@@ -1,2 -1,2 +1,3 @@@ func\n  ctx\n -old\n +new\n++both\n"
	want := "\x1b[1mdiff --cc file\x1b[0m\n" +
		"\x1b[36m@@@ -1,2 -1,2 +1,3 @@@\x1b[0m func\n" +
		"  ctx\n" +
		"\x1b[31m -old\x1b[0m\n" +
		"\x1b[32m +new\x1b[0m\n" +
		"\x1b[32m++both\x1b[0m\n"
	if got := th.PaintDiff(diff); got != want {
		t.Errorf("PaintDiff =\n%q\nwant\n%q", got, want)
	}
}

// What follows a file without a hunk is not painted as its header.
func TestPaintDiffAfterAFileWithoutAHunk(t *testing.T) {
	th := Theme{Added: "32", Removed: "31", DiffHeader: "1", DiffHunk: "36"}
	diff := "diff --git a/x b/y\nrename from x\nrename to y\n\ncommit 2222\n    message\n"
	want := "\x1b[1mdiff --git a/x b/y\x1b[0m\n\x1b[1mrename from x\x1b[0m\n\x1b[1mrename to y\x1b[0m\n\ncommit 2222\n    message\n"
	if got := th.PaintDiff(diff); got != want {
		t.Errorf("PaintDiff =\n%q\nwant\n%q", got, want)
	}
}

// PaintDiff takes nothing out of the diff. Text from elsewhere is made plain
// first, and then holds nothing but the diff's own text to paint.
func TestPaintDiffOfPlainText(t *testing.T) {
	th := Theme{Added: "32", Removed: "31", DiffHunk: "36"}
	diff := "@@ -1 +1 @@\n-a\x1b[0m still removed\n+\x1b[31mlooks removed\x1b]52;c;AAAA\x07\n"
	if got := th.PaintDiff(diff); !strings.Contains(got, "\x1b]52;c;AAAA\x07") {
		t.Errorf("PaintDiff took an escape sequence out of the diff: %q", got)
	}
	want := "\x1b[36m@@ -1 +1 @@\x1b[0m\n\x1b[31m-a still removed\x1b[0m\n\x1b[32m+looks removed\x1b[0m\n"
	if got := th.PaintDiff(term.Plain(diff)); got != want {
		t.Errorf("PaintDiff of plain text =\n%q\nwant\n%q", got, want)
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
