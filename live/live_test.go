package live

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/mittwald/cli-kit/term"
)

// Each draw moves back up over the lines the previous one took and erases
// them before writing its own; drawing the same lines again writes nothing.
func TestLiveRedrawsInPlace(t *testing.T) {
	var out bytes.Buffer
	live := New(&out)

	if err := live.Draw([]string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "\x1b[Ja\nb\n"; got != want {
		t.Errorf("first draw = %q, want %q", got, want)
	}

	out.Reset()
	_ = live.Draw([]string{"a", "b"})
	if out.Len() != 0 {
		t.Errorf("drawing the same lines wrote %q", out.String())
	}

	_ = live.Draw([]string{"c"})
	if got, want := out.String(), "\r\x1b[2A\x1b[Jc\n"; got != want {
		t.Errorf("redraw = %q, want %q", got, want)
	}
}

// A terminal made narrower between two draws has wrapped the previous block
// onto more rows, which the next draw moves back up over.
func TestLiveCountsRowsAtTheCurrentWidth(t *testing.T) {
	var out bytes.Buffer
	live := New(&out)
	width := 80
	live.size = func() (int, int) { return width, 24 }

	_ = live.Draw([]string{"a", "abcdefghij"})
	out.Reset()
	width = 4
	_ = live.Draw([]string{"b"})
	if got, want := out.String(), "\r\x1b[4A\x1b[Jb\n"; got != want {
		t.Errorf("redraw = %q, want %q", got, want)
	}
}

// A caller that reuses its slice for the next frame does not change what
// the next draw moves up over: the screen still shows the old lines.
func TestLiveKeepsItsOwnCopyOfTheDrawnLines(t *testing.T) {
	var out bytes.Buffer
	live := New(&out)
	live.size = func() (int, int) { return 4, 24 }

	frame := []string{"abcdefgh"}
	_ = live.Draw(frame)
	out.Reset()
	frame[0] = "x"
	_ = live.Draw(frame)
	if got, want := out.String(), "\r\x1b[2A\x1b[Jx\n"; got != want {
		t.Errorf("redraw = %q, want %q", got, want)
	}
}

// Lines taller than the screen lose their first lines, so the next draw can
// still move back up to where this one started; the cursor's row counts too.
func TestLiveKeepsToTheScreen(t *testing.T) {
	var out bytes.Buffer
	live := New(&out)
	live.size = func() (int, int) { return 4, 3 }

	if live.Fits([]string{"a", "b", "c"}) || !live.Fits([]string{"a", "b"}) || live.Fits([]string{"abcde", "b"}) {
		t.Error("Fits() does not keep a row for the cursor, or does not count wrapped lines")
	}
	_ = live.Draw([]string{"a", "b", "c", "d"})
	if got, want := out.String(), "\x1b[Jc\nd\n"; got != want {
		t.Errorf("draw = %q, want %q", got, want)
	}
	out.Reset()
	_ = live.Draw([]string{"e"})
	if got, want := out.String(), "\r\x1b[2A\x1b[Je\n"; got != want {
		t.Errorf("redraw = %q, want %q", got, want)
	}
}

// A single line that wraps past the bottom of the screen is cut short to the
// rows there are, so it can still be redrawn in place.
func TestLiveCutsALineTallerThanTheScreen(t *testing.T) {
	var out bytes.Buffer
	live := New(&out)
	live.size = func() (int, int) { return 4, 3 }

	_ = live.Draw([]string{"a", "abcdefghijklmnop"})
	if got, want := out.String(), "\x1b[Jabcdefg…\n"; got != want {
		t.Errorf("draw = %q, want %q", got, want)
	}
	out.Reset()
	_ = live.Draw([]string{"e"})
	if got, want := out.String(), "\r\x1b[2A\x1b[Je\n"; got != want {
		t.Errorf("redraw = %q, want %q", got, want)
	}
}

// A terminal one row high has no room to redraw in, so each draw goes below
// the previous one rather than up into lines already scrolled away.
func TestLiveOnASingleRow(t *testing.T) {
	var out bytes.Buffer
	live := New(&out)
	live.size = func() (int, int) { return 80, 1 }

	_ = live.Draw([]string{"a"})
	_ = live.Draw([]string{"b"})
	if got, want := out.String(), "\x1b[Ja\n\x1b[Jb\n"; got != want {
		t.Errorf("draws = %q, want %q", got, want)
	}
}

// Wide characters take two cells: five of them on a terminal four cells wide
// take three rows, not two.
func TestLiveCountsWideCharactersTwice(t *testing.T) {
	var out bytes.Buffer
	live := New(&out)
	live.size = func() (int, int) { return 4, 3 }

	if live.Fits([]string{"界界界界界"}) {
		t.Error("Fits() takes five wide characters for two rows of four cells")
	}
	_ = live.Draw([]string{"界界界界界"})
	if got, want := out.String(), "\x1b[J界界界…\n"; got != want {
		t.Errorf("draw = %q, want %q", got, want)
	}
	out.Reset()
	_ = live.Draw([]string{"e"})
	if got, want := out.String(), "\r\x1b[2A\x1b[Je\n"; got != want {
		t.Errorf("redraw = %q, want %q", got, want)
	}
}

// Clear takes the block back off the screen and forgets it, so the next draw
// starts where the block was.
func TestLiveClear(t *testing.T) {
	var out bytes.Buffer
	live := New(&out)
	live.size = func() (int, int) { return 80, 24 }

	_ = live.Draw([]string{"a", "b"})
	out.Reset()
	if err := live.Clear(); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "\r\x1b[2A\x1b[J"; got != want {
		t.Errorf("clear = %q, want %q", got, want)
	}
	out.Reset()
	_ = live.Clear()
	if out.Len() != 0 {
		t.Errorf("clearing twice wrote %q", out.String())
	}
	_ = live.Draw([]string{"a", "b"})
	if got, want := out.String(), "\x1b[Ja\nb\n"; got != want {
		t.Errorf("draw after clear = %q, want %q", got, want)
	}
}

func TestTruncateRows(t *testing.T) {
	const open, closeLink = "\x1b]8;;https://example.com/y\x1b\\", "\x1b]8;;\x1b\\"
	for _, c := range []struct {
		text        string
		width, rows int
		want        string
	}{
		{"abc", 3, 1, "abc"},
		{"abcdefgh", 5, 1, "abcd…"},
		{"abcdefghij", 4, 2, "abcdefg…"},
		{"\x1b[33mwarning: long\x1b[0m", 5, 1, "\x1b[33mwarn…\x1b[0m"},
		// The ellipsis takes the color of the text it stands for.
		{"ab\x1b[32mcdef\x1b[0m", 3, 1, "ab\x1b[32m…\x1b[0m"},
		{open + "https://example.com/y" + closeLink, 6, 1, open + "https…" + closeLink},
		{"界界界", 5, 1, "界界…"},
		// The ellipsis takes the cell a wide character did not fit into.
		{"界界界", 4, 1, "界…"},
	} {
		if got := truncateRows(c.text, c.width, c.rows); got != c.want {
			t.Errorf("truncateRows(%q, %d, %d) = %q, want %q", c.text, c.width, c.rows, got, c.want)
		}
	}
}

// A line wider than the terminal wraps onto more rows; colors and hyperlinks
// take up none.
func TestScreenRows(t *testing.T) {
	link := "\x1b]8;;https://x\x1b\\https://x\x1b]8;;\x1b\\"
	for _, c := range []struct {
		lines []string
		width int
		want  int
	}{
		{[]string{"abcd", ""}, 4, 2},
		{[]string{"abcde"}, 4, 2},
		{[]string{"\x1b[32mabcd\x1b[0m"}, 4, 1},
		{[]string{link}, 9, 1},
		{[]string{"(running) • 02m"}, 15, 1},
		{[]string{"abcdefghij"}, 0, 1},
		{[]string{"界界界界界"}, 4, 3},
		// A wide character that would straddle the edge starts the next row.
		{[]string{"a界"}, 2, 2},
		{[]string{"ab界界"}, 4, 2},
		{[]string{"🙂🙂"}, 3, 2},
		{[]string{"éé"}, 2, 1},
	} {
		if got := term.ScreenRows(c.lines, c.width); got != c.want {
			t.Errorf("ScreenRows(%q, %d) = %d, want %d", c.lines, c.width, got, c.want)
		}
	}
}

// A line with a newline in it is drawn as the lines between the newlines,
// and the next draw moves up over every one of them.
func TestLiveSplitsLinesAtNewlines(t *testing.T) {
	var out bytes.Buffer
	live := New(&out)
	live.size = func() (int, int) { return 80, 24 }

	_ = live.Draw([]string{"error: step failed\r\ncaused by: timeout", "retrying"})
	if got, want := out.String(), "\x1b[Jerror: step failed\ncaused by: timeout\nretrying\n"; got != want {
		t.Errorf("draw = %q, want %q", got, want)
	}
	out.Reset()
	_ = live.Draw([]string{"done"})
	if got, want := out.String(), "\r\x1b[3A\x1b[Jdone\n"; got != want {
		t.Errorf("redraw = %q, want %q", got, want)
	}

	live.size = func() (int, int) { return 80, 3 }
	if live.Fits([]string{"a\nb\nc"}) || !live.Fits([]string{"a\nb"}) {
		t.Error("Fits() does not count the lines a newline makes")
	}
}

// A tab moves to the next tab stop, which can push the text behind it onto
// a second row although it has few characters.
func TestLiveCountsTabStops(t *testing.T) {
	var out bytes.Buffer
	live := New(&out)
	live.size = func() (int, int) { return 12, 24 }

	_ = live.Draw([]string{"a\t0123456789"}) // 8 cells up to the tab stop, then 10
	out.Reset()
	_ = live.Draw([]string{"b"})
	if got, want := out.String(), "\r\x1b[2A\x1b[Jb\n"; got != want {
		t.Errorf("redraw = %q, want %q", got, want)
	}

	// Cut to one row, the tab counts the cells up to its stop as well.
	if got, want := truncateRows("ab\tcdefghij", 12, 1), "ab\tcde…"; got != want {
		t.Errorf("truncateRows = %q, want %q", got, want)
	}
}

// Drawing no lines takes the block off the screen and leaves no row behind
// that a later draw would have to go below.
func TestLiveDrawsNothingAsClear(t *testing.T) {
	for _, empty := range [][]string{nil, {}} {
		var out bytes.Buffer
		live := New(&out)
		live.size = func() (int, int) { return 80, 24 }

		_ = live.Draw([]string{"a"})
		out.Reset()
		if err := live.Draw(empty); err != nil {
			t.Fatal(err)
		}
		if got, want := out.String(), "\r\x1b[1A\x1b[J"; got != want {
			t.Errorf("empty draw = %q, want %q", got, want)
		}
		out.Reset()
		_ = live.Draw(empty)
		_ = live.Clear()
		if out.Len() != 0 {
			t.Errorf("empty draw on an empty block wrote %q", out.String())
		}
		_ = live.Draw([]string{"b"})
		if got, want := out.String(), "\x1b[Jb\n"; got != want {
			t.Errorf("draw after an empty draw = %q, want %q", got, want)
		}
	}
}

// A block far taller than the screen costs one look at the terminal and the
// lines that end up on it, not one of each per line dropped: this draw took
// most of a minute when every dropped line measured all the others again.
func TestLiveDropsLinesWithoutMeasuringThemAgain(t *testing.T) {
	var out bytes.Buffer
	live := New(&out)
	asked := 0
	live.size = func() (int, int) { asked++; return 120, 40 }

	lines := make([]string, 20000)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %05d %s", i, strings.Repeat("x", 70))
	}
	if err := live.Draw(lines); err != nil {
		t.Fatal(err)
	}
	if asked != 1 {
		t.Errorf("Draw asked for the terminal size %d times, want once", asked)
	}
	if got, want := strings.Count(out.String(), "\n"), 39; got != want {
		t.Errorf("drew %d lines on a screen of 40 rows, want %d", got, want)
	}
	if !strings.HasSuffix(out.String(), lines[len(lines)-1]+"\n") {
		t.Error("the last line is not the last one drawn")
	}
}
