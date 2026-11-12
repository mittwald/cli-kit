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

// DiffScanner tells the lines of a unified diff apart, one at a time. It
// reads the diff the way patch does: a hunk header says how many old and new
// lines the hunk holds, and until they are through, a line starting with "-"
// is a removed line even when it reads "--- something" — only outside a hunk
// does such a line name a file. A hunk header without line counts, as some
// tools write them, makes the hunk run until the next header.
//
// The zero DiffScanner is ready to read a diff from its first line.
type DiffScanner struct {
	// header is set inside a file's header: after its diff line, until its
	// first hunk.
	header bool
	// hunk is set inside a hunk; counted when its header gave the old and new
	// line counts, which old and new count down.
	hunk     bool
	counted  bool
	old, new int
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
	case strings.HasPrefix(line, "diff "):
		s.header = true
		return DiffFileHeader
	case s.header, strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
		return DiffFileHeader
	}
	return DiffText
}

// inHunk classifies a line inside a hunk. It reports false for a line that
// does not belong to the hunk, which ends it.
func (s *DiffScanner) inHunk(line string) (DiffKind, bool) {
	marker := byte(' ')
	if line != "" {
		marker = line[0]
	}
	if marker == '\\' {
		return DiffContext, true
	}
	if !s.counted {
		switch marker {
		case ' ':
			return DiffContext, true
		case '-':
			return DiffRemoved, true
		case '+':
			return DiffAdded, true
		}
		return 0, false
	}
	var kind DiffKind
	switch {
	case marker == ' ' && s.old > 0 && s.new > 0:
		s.old, s.new = s.old-1, s.new-1
		kind = DiffContext
	case marker == '-' && s.old > 0:
		s.old--
		kind = DiffRemoved
	case marker == '+' && s.new > 0:
		s.new--
		kind = DiffAdded
	default:
		return 0, false
	}
	if s.old == 0 && s.new == 0 {
		s.hunk, s.ended = false, true
	}
	return kind, true
}

// startHunk reads the line counts of a hunk header, @@ -start,count
// +start,count @@, where a count left out is 1.
func (s *DiffScanner) startHunk(line string) {
	s.header, s.hunk, s.counted = false, true, false
	fields := strings.Fields(line)
	if len(fields) < 4 || fields[0] != "@@" || fields[3] != "@@" {
		return
	}
	old, okOld := hunkCount(fields[1], '-')
	added, okNew := hunkCount(fields[2], '+')
	if !okOld || !okNew {
		return
	}
	s.counted, s.old, s.new = true, old, added
	if old == 0 && added == 0 {
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
func (t Theme) PaintDiffLine(kind DiffKind, line string) string {
	switch kind {
	case DiffFileHeader:
		return t.DiffHeader.Paint(line)
	case DiffHunkHeader:
		end := len(line)
		if i := strings.Index(line[min(2, len(line)):], "@@"); i >= 0 {
			end = min(2, len(line)) + i + 2
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
