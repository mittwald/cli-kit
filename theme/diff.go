package theme

import (
	"strconv"
	"strings"
)

// DiffKind is what a line of a unified diff is, as a [DiffScanner] tells it.
type DiffKind int

const (
	// DiffText is a line outside the files a diff changes, such as the
	// commit message above them. It is not painted.
	DiffText DiffKind = iota
	// DiffFileHeader is a line that introduces a file: diff --git, index,
	// mode and rename lines, and the --- and +++ lines naming the old and
	// the new file.
	DiffFileHeader
	// DiffHunkHeader is the line that starts a hunk: @@ -12,7 +12,8 @@.
	DiffHunkHeader
	// DiffContext is an unchanged line inside a hunk, or a "\ No newline at
	// end of file" note. It is not painted.
	DiffContext
	// DiffRemoved is a line the change takes away.
	DiffRemoved
	// DiffAdded is a line the change adds.
	DiffAdded
)

// maxParents is how many old files a [DiffScanner] counts the lines of in a
// combined diff. The hunks of a merge of more parents run until the next
// header, like those without line counts.
const maxParents = 8

// DiffScanner tells the lines of a unified diff apart, one at a time. It
// reads the diff the way patch does: a hunk header says how many old and new
// lines the hunk holds, and until they are through, a line starting with "-"
// is a removed line even when it reads "--- something" — only outside a hunk
// does such a line name a file. A hunk header without line counts, as some
// tools write them, makes the hunk run until the next header.
//
// A file's header starts at its diff line (diff --git, diff --cc, diff -u)
// and holds the lines git and diff write there: index, mode, similarity,
// rename and copy lines, the --- and +++ lines and the note that binary
// files differ. Any other line ends it, so what follows a file without a
// hunk, a renamed or a binary one, is text again: the next commit of a git
// log -p, say.
//
// The combined diff git shows for a merge is read as well. Its hunk headers
// have one more @ per further parent (@@@ -1,3 -1,3 +1,4 @@@), and its
// lines start with one marker column per parent: a line with a "-" in any
// column is removed, one with a "+" and no "-" is added.
//
// A DiffScanner reads its lines as they are and takes them for plain text:
// a line that starts with an escape sequence is not told apart. Diff text
// from elsewhere goes through term.Plain before it is classified, see
// [Theme.PaintDiff].
//
// The zero DiffScanner is ready to read a diff from its first line.
type DiffScanner struct {
	// header is set inside a file's header: after its diff line, until its
	// first hunk or the first line that belongs to no header.
	header bool
	// hunk is set inside a hunk; counted when its header gave the old and new
	// line counts, which old and new count down.
	hunk    bool
	counted bool
	// columns is how many marker columns the lines of the hunk start with:
	// one, or in a combined diff one per parent, each with a count in old.
	columns int
	old     [maxParents]int
	new     int
	// ended is set on the line that ended a hunk, which a "\ No newline at
	// end of file" note may still follow.
	ended bool
}

// Classify returns what line is, given the lines classified before it, and
// remembers it for the next. line is one line of the diff, without its
// newline.
func (s *DiffScanner) Classify(line string) DiffKind {
	if s.hunk {
		if kind, ok := s.inHunk(line); ok {
			return kind
		}
		s.hunk = false
	}
	ended := s.ended
	s.ended = false
	switch {
	case strings.HasPrefix(line, "@@"):
		s.startHunk(line)
		return DiffHunkHeader
	case ended && strings.HasPrefix(line, `\`):
		s.ended = true
		return DiffContext
	case strings.HasPrefix(line, "diff -"):
		s.header = true
		return DiffFileHeader
	case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
		return DiffFileHeader
	case s.header && headerLine(line):
		return DiffFileHeader
	}
	s.header = false
	return DiffText
}

// headerPrefixes start the lines of a file's header, between its diff line
// and its --- line: the extended header lines of git, of a combined diff
// too, and what diff and git say about binary files.
var headerPrefixes = []string{
	"index ", "mode ", "old mode ", "new mode ", "deleted file mode ", "new file mode ",
	"similarity index ", "dissimilarity index ", "rename from ", "rename to ", "copy from ", "copy to ",
	"Binary files ", "GIT binary patch",
}

// headerLine reports whether line belongs to a file's header.
func headerLine(line string) bool {
	for _, prefix := range headerPrefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// inHunk classifies a line inside a hunk. It reports false for a line that
// does not belong to the hunk, which ends it.
func (s *DiffScanner) inHunk(line string) (DiffKind, bool) {
	if strings.HasPrefix(line, `\`) {
		return DiffContext, true
	}
	// A line is removed when any of its columns says so, added when one
	// says so and none removes it. A column the line is too short for is a
	// space: mail strips the trailing space off an empty context line.
	removed, added := false, false
	for i := range s.columns {
		marker := byte(' ')
		if i < len(line) {
			marker = line[i]
		}
		switch marker {
		case ' ':
		case '-':
			removed = true
		case '+':
			added = true
		default:
			return 0, false
		}
	}
	kind := DiffContext
	switch {
	case removed:
		kind = DiffRemoved
	case added:
		kind = DiffAdded
	}
	if !s.counted {
		return kind, true
	}
	if !s.count(line, removed) {
		return 0, false
	}
	return kind, true
}

// count takes line off the line counts of the hunk, and ends the hunk when
// they are through. It reports false, and leaves the counts alone, for a
// line the hunk has no room for. A removed line is one of the old files
// that mark it with a "-". Any other line is one of the new file, and of
// each old file whose column is a space.
func (s *DiffScanner) count(line string, removed bool) bool {
	old, added := s.old, s.new
	if !removed {
		added--
	}
	through := added == 0
	for i := range s.columns {
		marker := byte(' ')
		if i < len(line) {
			marker = line[i]
		}
		if (removed && marker == '-') || (!removed && marker == ' ') {
			old[i]--
		}
		if old[i] < 0 {
			return false
		}
		through = through && old[i] == 0
	}
	if added < 0 {
		return false
	}
	s.old, s.new = old, added
	if through {
		s.hunk, s.ended = false, true
	}
	return true
}

// startHunk reads the line counts of a hunk header, @@ -start,count
// +start,count @@, where a count left out is 1. The header of a combined
// diff has one more @ on either side and one more old range per further
// parent: @@@ -start,count -start,count +start,count @@@.
func (s *DiffScanner) startHunk(line string) {
	ats := len(line) - len(strings.TrimLeft(line, "@"))
	s.header, s.hunk, s.counted, s.columns = false, true, false, ats-1
	fields := strings.Fields(line)
	if s.columns > maxParents || len(fields) < ats+2 || fields[0] != line[:ats] || fields[ats+1] != line[:ats] {
		return
	}
	through := true
	for i := range s.columns {
		old, ok := hunkCount(fields[1+i], '-')
		if !ok {
			return
		}
		s.old[i] = old
		through = through && old == 0
	}
	added, ok := hunkCount(fields[ats], '+')
	if !ok {
		return
	}
	s.counted, s.new = true, added
	if through && added == 0 {
		s.hunk = false
	}
}

// hunkCount reads one range of a hunk header, such as -12,7, and returns its
// line count.
func hunkCount(field string, sign byte) (int, bool) {
	if len(field) < 2 || field[0] != sign {
		return 0, false
	}
	start, count, found := strings.Cut(field[1:], ",")
	if _, err := strconv.Atoi(start); err != nil {
		return 0, false
	}
	if !found {
		return 1, true
	}
	n, err := strconv.Atoi(count)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// PaintDiffLine paints line, a line of a unified diff that a [DiffScanner]
// classified as kind: a file header in DiffHeader, the "@@ … @@" of a hunk
// header in DiffHunk — the function name git writes after it stays plain —,
// a removed line in Removed and an added one in Added. Text and context
// lines are returned unchanged.
//
// The line is painted as it is. An escape sequence in it is written out
// with it, and a reset in it ends the line's color early; see
// [Theme.PaintDiff] for diff text from elsewhere.
func (t Theme) PaintDiffLine(kind DiffKind, line string) string {
	switch kind {
	case DiffFileHeader:
		return t.DiffHeader.Paint(line)
	case DiffHunkHeader:
		// The header ends with the @ signs that close it, as many as open
		// it: three in a combined diff.
		open := len(line) - len(strings.TrimLeft(line, "@"))
		end := len(line)
		if i := strings.Index(line[open:], "@@"); i >= 0 {
			rest := line[open+i:]
			end = len(line) - len(strings.TrimLeft(rest, "@"))
		}
		return t.DiffHunk.Paint(line[:end]) + line[end:]
	case DiffRemoved:
		return t.Removed.Paint(line)
	case DiffAdded:
		return t.Added.Paint(line)
	}
	return line
}

// PaintDiff paints every line of text, a unified diff, the way
// [Theme.PaintDiffLine] does, telling the lines apart with a [DiffScanner].
// Each line is painted on its own, and line endings are kept. Paint with
// t.For(w) what is written to w, so it is painted only where w takes color:
//
//	fmt.Fprint(os.Stdout, theme.Active().For(os.Stdout).PaintDiff(diff))
//
// The text is painted as it is: nothing is taken out of it. A diff shows
// what files hold, and a file can hold escape sequences: one that sets the
// clipboard, or colors that make an added line look removed or hide it.
// Painting leaves them in place, and term.Sanitize would keep the colors.
// Diff text that does not come from the program itself goes through
// term.Plain first, which leaves the text and the line breaks of the diff
// and nothing else:
//
//	fmt.Fprint(os.Stdout, theme.Active().For(os.Stdout).PaintDiff(term.Plain(diff)))
//
// term.Plain reads a carriage return inside a line as a terminal does: the
// text after it draws over the text before it, the "+" or "-" of the line
// included. Where no changed line may pass for another, take the carriage
// returns out before, with strings.ReplaceAll(diff, "\r", "").
func (t Theme) PaintDiff(text string) string {
	if t.DiffHeader == "" && t.DiffHunk == "" && t.Removed == "" && t.Added == "" {
		return text
	}
	var scanner DiffScanner
	lines := strings.SplitAfter(text, "\n")
	for i, line := range lines {
		body := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		lines[i] = t.PaintDiffLine(scanner.Classify(body), body) + line[len(body):]
	}
	return strings.Join(lines, "")
}
