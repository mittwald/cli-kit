package pick

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

func testSwitches() *switches {
	return newSwitches(switchOptions{
		title:   "Which settings for %s?",
		subject: "tool 1.2.3",
		rows: []switchRow{
			{label: "Active", on: "active", off: "inactive"},
			{label: "Stable", on: "stable", off: "not stable", value: true},
		},
	}, false)
}

func testChecklist() *switches {
	return newSwitches(switchOptions{
		title: "Which tokens?",
		note:  "a note",
		rows: []switchRow{
			{label: "feed", note: "calendar", value: true},
			{label: "imap", note: "read mail"},
			{label: "smtp", note: "send mail"},
		},
		checklist: true,
	}, false)
}

func TestSwitchesThrowWhatTheCursorStandsOn(t *testing.T) {
	for _, testCase := range []struct {
		name string
		keys []key
		want []bool
	}{
		{name: "confirms what it started with", want: []bool{false, true}},
		{name: "Space throws the switch under the cursor", keys: []key{{kind: keyRune, rune: ' '}}, want: []bool{true, true}},
		{name: "Tab throws it too", keys: []key{{kind: keyTab}}, want: []bool{true, true}},
		{name: "the cursor moves and throws where it stands", keys: []key{{kind: keyDown}, {kind: keyTab}}, want: []bool{false, false}},
		{name: "throwing twice puts it back", keys: []key{{kind: keyTab}, {kind: keyRune, rune: ' '}}, want: []bool{false, true}},
		{name: "End reaches the last switch", keys: []key{{kind: keyEnd}, {kind: keyTab}}, want: []bool{false, false}},
		{name: "a turns all on", keys: []key{{kind: keyRune, rune: 'a'}}, want: []bool{true, true}},
		{name: "n turns all off", keys: []key{{kind: keyRune, rune: 'n'}}, want: []bool{false, false}},
		{name: "an ordinary rune changes nothing", keys: []key{{kind: keyRune, rune: 'x'}}, want: []bool{false, true}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			s := testSwitches()
			for _, k := range testCase.keys {
				if got := s.apply(k); got != actionNone {
					t.Fatalf("apply(%v) = %v, want the prompt to stay open", k, got)
				}
			}
			if got := s.values(); !slices.Equal(got, testCase.want) {
				t.Errorf("values() = %v, want %v", got, testCase.want)
			}
		})
	}
}

// The cursor stops at both ends rather than wrapping.
func TestSwitchesCursorStaysInRange(t *testing.T) {
	s := testSwitches()
	for range 3 {
		s.apply(key{kind: keyUp})
	}
	if s.cursor != 0 {
		t.Errorf("cursor = %d, want it held at the first switch", s.cursor)
	}
	for range 5 {
		s.apply(key{kind: keyDown})
	}
	if got, want := s.cursor, len(s.rows)-1; got != want {
		t.Errorf("cursor = %d, want it held at %d", got, want)
	}
}

func TestSwitchesEnterAndEscape(t *testing.T) {
	for _, s := range []*switches{testSwitches(), testChecklist()} {
		if got := s.apply(key{kind: keyEnter}); got != actionSelect {
			t.Errorf("Enter = %v, want the answer taken", got)
		}
		if got := s.apply(key{kind: keyInterrupt}); got != actionInterrupt {
			t.Errorf("Ctrl-C = %v, want the run interrupted", got)
		}
		if got := s.apply(key{kind: keyBack}); got != actionBack {
			t.Errorf("Esc = %v, want the prompt left for the one before", got)
		}
	}
}

// A row says which way it points, in the caller's own words, and a thrown
// switch keeps what it was next to what it became.
func TestSwitchesRenderStatesAndWhatChanged(t *testing.T) {
	s := testSwitches()
	lines := s.render(80)
	if got, want := lines[0], "Which settings for tool 1.2.3?"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	rows := strings.Join(lines, "\n")
	if !strings.Contains(rows, "❯ Active  [ ] inactive") {
		t.Errorf("rows = %q, want the cursor on an unset Active", rows)
	}
	if !strings.Contains(rows, "Stable  [x] stable") {
		t.Errorf("rows = %q, want Stable set", rows)
	}
	if strings.Contains(rows, "was ") {
		t.Errorf("rows = %q, want nothing said about a change that was not made", rows)
	}
	if !strings.HasSuffix(rows, "Esc cancel") {
		t.Errorf("rows = %q, want Esc named cancel", rows)
	}

	s.apply(key{kind: keyTab})
	if rows := strings.Join(s.render(80), "\n"); !strings.Contains(rows, "[x] active  · was inactive") {
		t.Errorf("rows = %q, want the thrown switch to say what it changed from", rows)
	}
	s.nested = true
	if got := s.hint(200); !strings.HasSuffix(got, "Esc back") {
		t.Errorf("nested hint = %q, want Esc named back", got)
	}
}

// A switch without words reads yes or no.
func TestSwitchWordFallsBackToYesAndNo(t *testing.T) {
	row := switchRow{label: "Retarget"}
	if got := switchWord(row, false); got != "no" {
		t.Errorf("switchWord(false) = %q, want no", got)
	}
	if got := switchWord(row, true); got != "yes" {
		t.Errorf("switchWord(true) = %q, want yes", got)
	}
}

func TestChecklistKeys(t *testing.T) {
	for _, tt := range []struct {
		name string
		keys []key
		want []bool
	}{
		{name: "confirms the defaults untouched", want: []bool{true, false, false}},
		{name: "Space ticks the row under the cursor", keys: []key{{kind: keyRune, rune: ' '}}, want: []bool{false, false, false}},
		{name: "Tab ticks too", keys: []key{{kind: keyDown}, {kind: keyTab}}, want: []bool{true, true, false}},
		{name: "j and k move", keys: []key{{kind: keyRune, rune: 'j'}, {kind: keyRune, rune: 'j'}, {kind: keyRune, rune: 'k'}, {kind: keyRune, rune: ' '}}, want: []bool{true, true, false}},
		{name: "the cursor stops at the end", keys: []key{{kind: keyDown}, {kind: keyDown}, {kind: keyDown}, {kind: keyTab}}, want: []bool{true, false, true}},
		{name: "the cursor stops at the top", keys: []key{{kind: keyUp}, {kind: keyTab}}, want: []bool{false, false, false}},
		{name: "a ticks all", keys: []key{{kind: keyRune, rune: 'a'}}, want: []bool{true, true, true}},
		{name: "n clears all", keys: []key{{kind: keyRune, rune: 'n'}}, want: []bool{false, false, false}},
		{name: "other runes change nothing", keys: []key{{kind: keyRune, rune: 'x'}}, want: []bool{true, false, false}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := testChecklist()
			for _, k := range tt.keys {
				if got := c.apply(k); got != actionNone {
					t.Fatalf("apply(%v) = %v, want the prompt to stay open", k, got)
				}
			}
			if got := c.values(); !slices.Equal(got, tt.want) {
				t.Errorf("values() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChecklistRender(t *testing.T) {
	c := testChecklist()
	c.apply(key{kind: keyDown})
	got := strings.Join(c.render(100), "\n")
	want := strings.Join([]string{
		"Which tokens?",
		"a note",
		"",
		"  [x] feed  calendar",
		"❯ [ ] imap  read mail",
		"  [ ] smtp  send mail",
		"",
		"↑/↓ move · Space/Tab tick · a all · n none · Enter confirm · Esc cancel",
	}, "\n")
	if got != want {
		t.Errorf("render:\n%s\nwant:\n%s", got, want)
	}
	for _, line := range c.render(12) {
		if width(line) > 12 {
			t.Errorf("line %q is wider than 12 cells", line)
		}
	}
	// A box without a note is not padded.
	bare := newSwitches(switchOptions{rows: []switchRow{{label: "only"}}, checklist: true}, false)
	if got := bare.render(80)[2]; got != "❯ [ ] only" {
		t.Errorf("row = %q, want no padding", got)
	}
}

func TestChecklistColor(t *testing.T) {
	c := testChecklist()
	c.color = true
	lines := c.render(80)
	if !strings.Contains(lines[3], th.SelectedAction.Paint("[x] ")) {
		t.Errorf("ticked box under the cursor not highlighted: %q", lines[3])
	}
	c.apply(key{kind: keyDown})
	if !strings.Contains(c.render(80)[3], th.Action.Paint("[x] ")) {
		t.Errorf("ticked box not in the action style: %q", c.render(80)[3])
	}
}

func TestDriveSwitches(t *testing.T) {
	c := testChecklist()
	err := drive(c, &renderer{w: io.Discard}, fixedSize(80, 24), 0, &scriptedInput{chunks: [][]byte{[]byte("\x1b[B"), []byte(" \r")}})
	if err != nil {
		t.Fatalf("drive: %v", err)
	}
	if got := c.values(); !slices.Equal(got, []bool{true, true, false}) {
		t.Errorf("values() = %v", got)
	}
	if err := drive(testChecklist(), &renderer{w: io.Discard}, fixedSize(80, 24), 0, &scriptedInput{chunks: [][]byte{{0x1b}}}); !errors.Is(err, ErrBack) {
		t.Errorf("Esc: err = %v, want ErrBack", err)
	}
	if err := drive(testChecklist(), &renderer{w: io.Discard}, fixedSize(80, 24), 0, &scriptedInput{}); !errors.Is(err, ErrInterrupted) {
		t.Errorf("EOF: err = %v, want ErrInterrupted", err)
	}
}
