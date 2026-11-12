package pick

import (
	"errors"
	"io"
	"testing"
)

func TestDecodeKeys(t *testing.T) {
	for _, c := range []struct {
		in       string
		kind     keyKind
		rune     rune
		consumed int
	}{
		{"\r", keyEnter, 0, 1},
		{"\n", keyEnter, 0, 1},
		{"\r\n", keyEnter, 0, 2},
		{"\r\r", keyEnter, 0, 1},
		{"\t", keyTab, 0, 1},
		{"\x1b[Z", keyBackTab, 0, 3},
		{"\x7f", keyBackspace, 0, 1},
		{"\x08", keyBackspace, 0, 1},
		{"\x03", keyInterrupt, 0, 1},
		{"\x04", keyInterrupt, 0, 1},
		{"\x15", keyClearFilter, 0, 1},
		{"\x01", keyHome, 0, 1},
		{"\x05", keyEnd, 0, 1},
		{"\x0b", keyKillToEnd, 0, 1},
		{"\x17", keyDeleteWord, 0, 1},
		{"\x0e", keyDown, 0, 1},
		{"\x10", keyUp, 0, 1},
		{"\x1b[A", keyUp, 0, 3},
		{"\x1b[B", keyDown, 0, 3},
		{"\x1b[C", keyRight, 0, 3},
		{"\x1b[D", keyLeft, 0, 3},
		{"\x1bOA", keyUp, 0, 3},
		{"\x1b[H", keyHome, 0, 3},
		{"\x1b[F", keyEnd, 0, 3},
		{"\x1b[1~", keyHome, 0, 4},
		{"\x1b[4~", keyEnd, 0, 4},
		{"\x1b[3~", keyDelete, 0, 4},
		{"\x1b[5~", keyPageUp, 0, 4},
		{"\x1b[6~", keyPageDown, 0, 4},
		{"\x1b[1;5A", keyUp, 0, 6},
		{"\x1b[99~", keyOther, 0, 5},
		{"\x1bx", keyOther, 0, 2},
		{"a", keyRune, 'a', 1},
		{"ä", keyRune, 'ä', 2},
		{"日本", keyRune, '日', 3},
		{"\x00", keyOther, 0, 1},
		// A modifier in the parameters does not change the key.
		{"\x1b[3;5~", keyDelete, 0, 6},
		{"\x1bO5A", keyUp, 0, 4},
		// Alt sent as an ESC in front of the key's own sequence.
		{"\x1b\x1b[D", keyLeft, 0, 4},
		{"\x1b\x1b[5~", keyPageUp, 0, 5},
		{"\x1b\x1bOA", keyUp, 0, 4},
		{"\x1b\x1bx", keyOther, 0, 3},
		{"\x1bä", keyOther, 0, 3},
		// The Linux console's F1: the letter belongs to the key.
		{"\x1b[[A", keyOther, 0, 4},
		{"\x1b[[Ax", keyOther, 0, 4},
		// Intermediate bytes belong to the sequence too.
		{"\x1b[1 q", keyOther, 0, 5},
		{"\x1b[?1;2c", keyOther, 0, 7},
		// Alt-[ and Alt-Shift-O with another key behind them: the key is
		// left to be decoded on its own.
		{"\x1b[\r", keyOther, 0, 2},
		{"\x1b[\x03", keyOther, 0, 2},
		{"\x1b[\x1b[A", keyOther, 0, 2},
		{"\x1bO\r", keyOther, 0, 2},
		{"\x1bO~", keyOther, 0, 3},
		// A C1 control and a character that reorders text are no input.
		{"\u009b", keyOther, 0, 2},
		{"\u202e", keyOther, 0, 3},
		{"\u2028", keyOther, 0, 3},
		{"\u00a0", keyRune, '\u00a0', 2},
		{"\u200d", keyRune, '\u200d', 3},
	} {
		k, consumed := decode([]byte(c.in), true)
		if k.kind != c.kind || k.rune != c.rune || consumed != c.consumed {
			t.Errorf("decode(%q) = %+v, %d; want kind %d rune %q, %d", c.in, k, consumed, c.kind, c.rune, c.consumed)
		}
	}
}

// A sequence split across reads is not decided until the rest arrives.
func TestDecodeWaitsForSplitSequences(t *testing.T) {
	for _, in := range []string{"\x1b", "\x1b[", "\x1b[1;5", "\x1b[1 ", "\x1b[[", "\x1bO", "\x1b\x1b", "\x1b\x1b[", "\x1b\xc3", "\xe6\x97"} {
		if _, consumed := decode([]byte(in), true); consumed != 0 {
			t.Errorf("decode(%q, more) consumed %d, want 0", in, consumed)
		}
	}
	// Without more to come, a dangling partial rune is dropped.
	if k, consumed := decode([]byte("\xe6\x97"), false); consumed != 1 || k.kind != keyOther {
		t.Errorf("decode(partial rune, no more) = %+v, %d; want it dropped", k, consumed)
	}
}

// A lone ESC with nothing to follow is the Esc key.
func TestDecodeLoneEscapeGoesBack(t *testing.T) {
	if k, consumed := decode([]byte{0x1b}, false); k.kind != keyBack || consumed != 1 {
		t.Errorf("decode(ESC) = %+v, %d; want keyBack, 1", k, consumed)
	}
}

func TestDecodeEmpty(t *testing.T) {
	if _, consumed := decode(nil, true); consumed != 0 {
		t.Errorf("decode(nil) consumed %d", consumed)
	}
}

// With nothing more to come, whatever is pending is decided: a sequence that
// never completes is dropped whole instead of waiting for, and swallowing,
// the key pressed next.
func TestDecodeResolvesWhatIsPending(t *testing.T) {
	for _, c := range []struct {
		in       string
		kind     keyKind
		consumed int
	}{
		{"\x1b[", keyOther, 2},
		{"\x1bO", keyOther, 2},
		{"\x1b[1;5", keyOther, 5},
		{"\x1b[[", keyOther, 3},
		{"\x1b\x1b", keyBack, 2},
		{"\x1b\x1b[", keyOther, 3},
		{"\x1b\xc3", keyOther, 2},
	} {
		k, consumed := decode([]byte(c.in), false)
		if k.kind != c.kind || consumed != c.consumed {
			t.Errorf("decode(%q, no more) = %+v, %d; want kind %d, %d", c.in, k, consumed, c.kind, c.consumed)
		}
	}
}

// No sequence a terminal sends for a key ends up as text in a prompt, and
// none takes the key pressed after it along.
func TestDriveKeepsEscapeSequencesOutOfTheAnswer(t *testing.T) {
	for _, c := range []struct {
		name   string
		chunks []string
		want   string
	}{
		{"Alt-Left as ESC ESC [ D moves the caret", []string{"pw\x1b\x1b[Dx\r"}, "pxw"},
		{"the Linux console's F1", []string{"pw\x1b[[A\r"}, "pw"},
		{"a sequence with intermediate bytes", []string{"pw\x1b[1 q\r"}, "pw"},
		{"Alt-[ leaves Enter alone", []string{"ab", "\x1b[", "\r", "x", "\r"}, "ab"},
		{"Alt-Shift-O leaves Enter alone", []string{"ab", "\x1bO", "\r", "x", "\r"}, "ab"},
		{"a pasted C1 control", []string{"a\u009b2Jb\r"}, "a2Jb"},
	} {
		t.Run(c.name, func(t *testing.T) {
			source := &scriptedInput{noGrace: true}
			for _, chunk := range c.chunks {
				source.chunks = append(source.chunks, []byte(chunk))
			}
			e := newEditor(InputOptions{Prompt: "Password: ", Secret: true}, false)
			if err := drive(e, &renderer{w: io.Discard}, fixedSize(80, 24), 0, source); err != nil {
				t.Fatalf("drive() error = %v", err)
			}
			if got := e.answer(); got != c.want {
				t.Errorf("answer() = %q, want %q", got, c.want)
			}
		})
	}

	l := testList(t, 5, []string{"one"}, []string{"two"})
	source := &scriptedInput{noGrace: true, chunks: [][]byte{[]byte("\x1b["), {0x03}, []byte("\r")}}
	if err := drive(l, &renderer{w: io.Discard}, fixedSize(80, 24), 0, source); !errors.Is(err, ErrInterrupted) {
		t.Errorf("Alt-[ then Ctrl-C: drive() error = %v, want ErrInterrupted", err)
	}
	l = testList(t, 5, []string{"one"}, []string{"two"})
	source = &scriptedInput{noGrace: true, chunks: [][]byte{[]byte("\x1b\x1b[B"), []byte("\r")}}
	if err := drive(l, &renderer{w: io.Discard}, fixedSize(80, 24), 0, source); err != nil || l.selected() != 1 || len(l.filter) != 0 {
		t.Errorf("Alt-Down then Enter: err %v, selected %d, filter %q; want row 1 and no filter", err, l.selected(), string(l.filter))
	}
}
