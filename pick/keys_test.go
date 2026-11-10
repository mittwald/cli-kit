package pick

import "testing"

func TestDecodeKeys(t *testing.T) {
	for _, c := range []struct {
		in       string
		kind     keyKind
		rune     rune
		consumed int
	}{
		{"\r", keyEnter, 0, 1},
		{"\n", keyEnter, 0, 1},
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
	} {
		k, consumed := decode([]byte(c.in), true)
		if k.kind != c.kind || k.rune != c.rune || consumed != c.consumed {
			t.Errorf("decode(%q) = %+v, %d; want kind %d rune %q, %d", c.in, k, consumed, c.kind, c.rune, c.consumed)
		}
	}
}

// A sequence split across reads is not decided until the rest arrives.
func TestDecodeWaitsForSplitSequences(t *testing.T) {
	for _, in := range []string{"\x1b", "\x1b[", "\x1b[1;5", "\xe6\x97"} {
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
