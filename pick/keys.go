package pick

import (
	"strings"
	"unicode/utf8"
)

// keyKind classifies one decoded keypress. Keys a prompt ignores decode to
// keyOther so the read loop still consumes their bytes, rather than leaving
// them to arrive as typed text.
type keyKind int

const (
	keyOther keyKind = iota
	keyRune
	keyUp
	keyDown
	keyLeft
	keyRight
	keyPageUp
	keyPageDown
	keyHome
	keyEnd
	keyEnter
	keyTab
	keyBackTab
	keyBackspace
	keyDelete
	keyDeleteWord
	keyKillToEnd
	keyClearFilter
	// keyBack is Esc: leave this prompt and return to the one before it.
	keyBack
	// keyInterrupt is Ctrl-C or Ctrl-D: leave the whole run.
	keyInterrupt
)

// key is a decoded keypress; rune is set only for keyRune.
type key struct {
	kind keyKind
	rune rune
}

// decode reads the first keypress from buf and reports how many bytes it
// consumed. A consumed count of 0 means buf holds the start of a longer
// sequence (an escape sequence split across reads, or a partial UTF-8 rune)
// and the caller has to read more before decoding again.
//
// A lone ESC is the Esc key, so it can only be decided once no continuation
// arrived: callers pass more=false to resolve what is left. Terminals write
// a whole escape sequence at once, so a real arrow key does not resolve as
// Esc in practice. With more=false every buf that holds anything is
// consumed from, so a sequence that never completes cannot hold up the keys
// behind it.
func decode(buf []byte, more bool) (key, int) {
	if len(buf) == 0 {
		return key{}, 0
	}
	switch buf[0] {
	case 0x1b:
		return decodeEscape(buf, more)
	case '\r':
		// A line pasted with its CRLF ending is one Enter, not two: the
		// second would answer the prompt after this one.
		if len(buf) > 1 && buf[1] == '\n' {
			return key{kind: keyEnter}, 2
		}
		return key{kind: keyEnter}, 1
	case '\n':
		return key{kind: keyEnter}, 1
	case '\t':
		return key{kind: keyTab}, 1
	case 0x7f, 0x08:
		return key{kind: keyBackspace}, 1
	case 0x03, 0x04: // Ctrl-C, Ctrl-D
		return key{kind: keyInterrupt}, 1
	case 0x01: // Ctrl-A
		return key{kind: keyHome}, 1
	case 0x05: // Ctrl-E
		return key{kind: keyEnd}, 1
	case 0x0b: // Ctrl-K
		return key{kind: keyKillToEnd}, 1
	case 0x17: // Ctrl-W
		return key{kind: keyDeleteWord}, 1
	case 0x0e: // Ctrl-N
		return key{kind: keyDown}, 1
	case 0x10: // Ctrl-P
		return key{kind: keyUp}, 1
	case 0x15: // Ctrl-U
		return key{kind: keyClearFilter}, 1
	}
	if buf[0] < 0x20 {
		return key{}, 1
	}
	r, size := utf8.DecodeRune(buf)
	if r == utf8.RuneError && size <= 1 {
		// Either an invalid byte, which is dropped, or a rune split across
		// reads, which the caller completes.
		if !utf8.FullRune(buf) && more {
			return key{}, 0
		}
		return key{}, 1
	}
	if !typable(r) {
		// A C1 control or a character that reorders text, pasted rather than
		// typed: drawing it would not show what was entered.
		return key{}, size
	}
	return key{kind: keyRune, rune: r}, size
}

// decodeEscape decodes what an ESC introduces: the CSI and SS3 sequences the
// arrow, paging, home/end, delete and Shift-Tab keys emit. Anything else is
// consumed whole and ignored, so an unknown sequence cannot leak into a
// prompt as text.
//
// A terminal that sends Alt as "ESC +" puts an ESC in front of the key's own
// sequence, so Alt-Left arrives as ESC ESC [ D. Every ESC but the last is
// such a prefix: the key is decoded without it, and Alt-Left moves left.
func decodeEscape(buf []byte, more bool) (key, int) {
	prefix := 0
	for prefix+1 < len(buf) && buf[prefix+1] == 0x1b {
		prefix++
	}
	k, consumed := decodeSequence(buf[prefix:], more)
	if consumed == 0 {
		return key{}, 0
	}
	return k, prefix + consumed
}

// decodeSequence decodes buf, which starts with ESC and holds no second one
// right behind it.
func decodeSequence(buf []byte, more bool) (key, int) {
	if len(buf) == 1 {
		if more {
			return key{}, 0
		}
		return key{kind: keyBack}, 1
	}
	switch buf[1] {
	case '[':
		return decodeCSI(buf, more)
	case 'O':
		return decodeSS3(buf, more)
	}
	// ESC followed by an ordinary key: Alt-<key>, which is ignored together
	// with every byte of the key.
	if !utf8.FullRune(buf[1:]) && more {
		return key{}, 0
	}
	_, size := utf8.DecodeRune(buf[1:])
	return key{}, 1 + size
}

// decodeCSI decodes a control sequence: ESC [, parameter bytes (0x30–0x3F),
// intermediate bytes (0x20–0x2F) and one final byte (0x40–0x7E).
func decodeCSI(buf []byte, more bool) (key, int) {
	end := 2
	for end < len(buf) && buf[end] >= 0x30 && buf[end] <= 0x3f {
		end++
	}
	params := string(buf[2:end])
	intermediates := end
	for end < len(buf) && buf[end] >= 0x20 && buf[end] <= 0x2f {
		end++
	}
	if end >= len(buf) {
		if more {
			return key{}, 0
		}
		// Nothing more is coming: Alt-[, or a sequence cut off for good.
		return key{}, len(buf)
	}
	final := buf[end]
	if final < 0x40 || final > 0x7e {
		// No sequence after all, such as Alt-[ with another key behind it:
		// what was read goes, and the key behind it is decoded on its own.
		return key{}, end
	}
	consumed := end + 1
	if final == '[' && end == 2 {
		// The Linux console sends F1 to F5 as ESC [ [ A to ESC [ [ E: the
		// letter belongs to the key.
		if consumed >= len(buf) {
			if more {
				return key{}, 0
			}
			return key{}, consumed
		}
		if buf[consumed] >= 'A' && buf[consumed] <= 'E' {
			consumed++
		}
		return key{}, consumed
	}
	if end > intermediates {
		return key{}, consumed
	}
	return sequenceKey(final, params), consumed
}

// decodeSS3 decodes a single-shift sequence, which keypads in application
// mode send: ESC O and one final byte, with a modifier digit between them on
// some terminals.
func decodeSS3(buf []byte, more bool) (key, int) {
	end := 2
	for end < len(buf) && buf[end] >= 0x30 && buf[end] <= 0x3f {
		end++
	}
	if end >= len(buf) {
		if more {
			return key{}, 0
		}
		// Nothing more is coming: Alt-Shift-O.
		return key{}, len(buf)
	}
	if final := buf[end]; final >= 0x40 && final <= 0x7e {
		return sequenceKey(final, ""), end + 1
	}
	return key{}, end
}

// sequenceKey is the key a sequence stands for, by its final byte and, for
// the keys that share ~, its first parameter. A modifier in the parameters
// ("1;5" for Ctrl) is not told apart: Ctrl-Up is Up.
func sequenceKey(final byte, params string) key {
	switch final {
	case 'A':
		return key{kind: keyUp}
	case 'B':
		return key{kind: keyDown}
	case 'C':
		return key{kind: keyRight}
	case 'D':
		return key{kind: keyLeft}
	case 'H':
		return key{kind: keyHome}
	case 'F':
		return key{kind: keyEnd}
	case 'Z':
		return key{kind: keyBackTab}
	case '~':
		number, _, _ := strings.Cut(params, ";")
		switch number {
		case "1", "7":
			return key{kind: keyHome}
		case "4", "8":
			return key{kind: keyEnd}
		case "3":
			return key{kind: keyDelete}
		case "5":
			return key{kind: keyPageUp}
		case "6":
			return key{kind: keyPageDown}
		}
	}
	return key{}
}
