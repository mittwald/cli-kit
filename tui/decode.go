package tui

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// pasteStart and pasteEnd frame pasted text with bracketed paste on.
const (
	pasteStart = "\x1b[200~"
	pasteEnd   = "\x1b[201~"
)

// decode reads the first event from buf and reports how many bytes it
// consumed. The event is a Key, a Paste or a Mouse, or nil for bytes that
// mean nothing to the application (an unknown escape sequence, a focus
// report, an invalid byte), which are consumed all the same.
//
// A consumed count of 0 means buf holds the start of a longer sequence (an
// escape sequence or a paste split across reads, or a partial UTF-8 rune)
// and the caller has to read more first. A lone ESC can only be told from the
// start of a sequence once nothing followed it: callers pass more=false to
// decode what is left, and then every non-empty buf is consumed from.
func decode(buf []byte, more bool) (any, int) {
	if len(buf) == 0 {
		return nil, 0
	}
	if buf[0] == 0x1b {
		return decodeEscape(buf, more)
	}
	return decodePlain(buf, more)
}

// decodePlain decodes a byte that does not start an escape sequence: a
// control character or a UTF-8 character.
func decodePlain(buf []byte, more bool) (any, int) {
	c := buf[0]
	switch c {
	case '\r':
		// A pasted line ending in CRLF without bracketed paste is one Enter.
		if len(buf) > 1 && buf[1] == '\n' {
			return Key{Code: KeyEnter}, 2
		}
		return Key{Code: KeyEnter}, 1
	case '\n':
		return Key{Code: KeyEnter}, 1
	case '\t':
		return Key{Code: KeyTab}, 1
	case 0x7f, 0x08:
		return Key{Code: KeyBackspace}, 1
	case 0x00:
		return Key{Code: KeyRune, Rune: ' ', Mod: ModCtrl}, 1
	}
	if c < 0x1b {
		return Key{Code: KeyRune, Rune: rune('a' + c - 1), Mod: ModCtrl}, 1
	}
	if c < 0x20 {
		// Ctrl-\, Ctrl-], Ctrl-^ and Ctrl-_.
		return Key{Code: KeyRune, Rune: rune('\\' + c - 0x1c), Mod: ModCtrl}, 1
	}
	r, size := utf8.DecodeRune(buf)
	if r == utf8.RuneError && size <= 1 {
		if !utf8.FullRune(buf) && more {
			return nil, 0
		}
		return nil, 1
	}
	if !textRune(r) {
		// A C1 control or another character that would not show what was
		// typed.
		return nil, size
	}
	return Key{Code: KeyRune, Rune: r}, size
}

// decodeEscape decodes what an ESC introduces.
func decodeEscape(buf []byte, more bool) (any, int) {
	if len(buf) == 1 {
		if more {
			return nil, 0
		}
		return Key{Code: KeyEscape}, 1
	}
	switch buf[1] {
	case '[':
		return decodeCSI(buf, more)
	case 'O':
		return decodeSS3(buf, more)
	case 0x1b:
		// ESC ESC: Alt held with a key that sends a sequence of its own, as
		// terminals that send Alt as an ESC prefix do with Alt-Up.
		ev, n := decodeEscape(buf[1:], more)
		if n == 0 {
			return nil, 0
		}
		if k, ok := ev.(Key); ok {
			k.Mod |= ModAlt
			return k, n + 1
		}
		return ev, n + 1
	}
	// ESC and an ordinary key: that key with Alt.
	ev, n := decodePlain(buf[1:], more)
	if n == 0 {
		return nil, 0
	}
	if k, ok := ev.(Key); ok {
		k.Mod |= ModAlt
		return k, n + 1
	}
	return nil, n + 1
}

// decodeSS3 decodes ESC O and one final byte: arrows and Home/End in
// application cursor mode, and F1 to F4.
func decodeSS3(buf []byte, more bool) (any, int) {
	if len(buf) < 3 {
		if more {
			return nil, 0
		}
		return Key{Code: KeyRune, Rune: 'O', Mod: ModAlt}, 2
	}
	switch buf[2] {
	case 'P':
		return Key{Code: KeyF1}, 3
	case 'Q':
		return Key{Code: KeyF2}, 3
	case 'R':
		return Key{Code: KeyF3}, 3
	case 'S':
		return Key{Code: KeyF4}, 3
	case 'M':
		return Key{Code: KeyEnter}, 3
	}
	if code, ok := letterKeys[buf[2]]; ok {
		return Key{Code: code}, 3
	}
	return nil, 3
}

// letterKeys are the keys of a CSI or SS3 sequence named by its final letter.
var letterKeys = map[byte]KeyCode{
	'A': KeyUp,
	'B': KeyDown,
	'C': KeyRight,
	'D': KeyLeft,
	'H': KeyHome,
	'F': KeyEnd,
	'P': KeyF1,
	'Q': KeyF2,
	'R': KeyF3,
	'S': KeyF4,
}

// tildeKeys are the keys of a CSI sequence ending in "~", by its number.
var tildeKeys = map[int]KeyCode{
	1: KeyHome, 2: KeyInsert, 3: KeyDelete, 4: KeyEnd, 5: KeyPageUp, 6: KeyPageDown,
	7: KeyHome, 8: KeyEnd,
	11: KeyF1, 12: KeyF2, 13: KeyF3, 14: KeyF4, 15: KeyF5,
	17: KeyF6, 18: KeyF7, 19: KeyF8, 20: KeyF9, 21: KeyF10, 23: KeyF11, 24: KeyF12,
}

// decodeCSI decodes ESC [, parameter bytes (0x30–0x3F), intermediate bytes
// (0x20–0x2F) and one final byte (0x40–0x7E).
func decodeCSI(buf []byte, more bool) (any, int) {
	if strings.HasPrefix(string(buf), pasteStart) {
		return decodePaste(buf, more)
	}
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
		if more && len(buf) < 64 {
			return nil, 0
		}
		return nil, len(buf)
	}
	final := buf[end]
	if final < 0x40 || final > 0x7e {
		return nil, end
	}
	consumed := end + 1
	if final == '[' && end == 2 {
		// The Linux console sends F1 to F5 as ESC [ [ A to ESC [ [ E.
		if consumed >= len(buf) {
			if more {
				return nil, 0
			}
			return nil, consumed
		}
		if c := buf[consumed]; c >= 'A' && c <= 'E' {
			return Key{Code: KeyF1 + KeyCode(c-'A')}, consumed + 1
		}
		return nil, consumed
	}
	if end > intermediates {
		return nil, consumed
	}
	if strings.HasPrefix(params, "<") && (final == 'M' || final == 'm') {
		return decodeMouse(params[1:], final), consumed
	}
	fields := strings.Split(params, ";")
	mod := Mod(0)
	if len(fields) > 1 {
		mod = modifiers(fields[1])
	}
	switch final {
	case '~':
		n, err := strconv.Atoi(fields[0])
		if err != nil {
			return nil, consumed
		}
		if code, ok := tildeKeys[n]; ok {
			return Key{Code: code, Mod: mod}, consumed
		}
		return nil, consumed
	case 'Z':
		return Key{Code: KeyTab, Mod: ModShift}, consumed
	case 'u':
		// The fixterms and kitty encoding: the code point and the
		// modifiers.
		return decodeCSIu(fields, mod), consumed
	}
	if code, ok := letterKeys[final]; ok && !strings.ContainsAny(params, "<>?=") {
		return Key{Code: code, Mod: mod}, consumed
	}
	return nil, consumed
}

// modifiers decodes the modifier parameter of a sequence: 1 plus a bit set
// of Shift (1), Alt (2) and Ctrl (4). Meta is taken for Alt.
func modifiers(field string) Mod {
	field, _, _ = strings.Cut(field, ":")
	n, err := strconv.Atoi(field)
	if err != nil || n < 2 {
		return 0
	}
	n--
	var mod Mod
	if n&1 != 0 {
		mod |= ModShift
	}
	if n&(2|8) != 0 {
		mod |= ModAlt
	}
	if n&4 != 0 {
		mod |= ModCtrl
	}
	return mod
}

// decodeCSIu decodes the fields of CSI code ; modifiers u.
func decodeCSIu(fields []string, mod Mod) any {
	first, _, _ := strings.Cut(fields[0], ":")
	code, err := strconv.Atoi(first)
	if err != nil || code < 0 || code > unicode.MaxRune {
		return nil
	}
	switch code {
	case 13:
		return Key{Code: KeyEnter, Mod: mod}
	case 9:
		return Key{Code: KeyTab, Mod: mod}
	case 27:
		return Key{Code: KeyEscape, Mod: mod}
	case 127, 8:
		return Key{Code: KeyBackspace, Mod: mod}
	}
	r := rune(code)
	if !unicode.IsPrint(r) {
		return nil
	}
	// Shift alone with a letter types the capital letter, as without
	// this encoding; with Ctrl or Alt it stays a modifier of the letter,
	// as "ctrl+shift+z" names the key.
	if mod&ModShift != 0 && unicode.IsLetter(r) {
		r = unicode.ToLower(r)
		if mod&(ModCtrl|ModAlt) == 0 {
			r = unicode.ToUpper(r)
			mod &^= ModShift
		}
	}
	return Key{Code: KeyRune, Rune: r, Mod: mod}
}

// decodeMouse decodes the parameters of an SGR mouse report, CSI < b ; x ; y
// M for a press or m for a release.
func decodeMouse(params string, final byte) any {
	fields := strings.Split(params, ";")
	if len(fields) != 3 {
		return nil
	}
	var n [3]int
	for i, f := range fields {
		v, err := strconv.Atoi(f)
		if err != nil || v < 0 {
			return nil
		}
		n[i] = v
	}
	b := n[0]
	m := Mouse{X: n[1] - 1, Y: n[2] - 1}
	if m.X < 0 || m.Y < 0 {
		return nil
	}
	if b&4 != 0 {
		m.Mod |= ModShift
	}
	if b&8 != 0 {
		m.Mod |= ModAlt
	}
	if b&16 != 0 {
		m.Mod |= ModCtrl
	}
	switch {
	case b&64 != 0:
		if b&1 == 0 {
			m.Button = MouseWheelUp
		} else {
			m.Button = MouseWheelDown
		}
	case b&3 == 0:
		m.Button = MouseLeft
	case b&3 == 1:
		m.Button = MouseMiddle
	case b&3 == 2:
		m.Button = MouseRight
	}
	switch {
	case b&32 != 0:
		m.Action = MouseMotion
	case final == 'm':
		m.Action = MouseRelease
	}
	return m
}

// maxPaste bounds the text one paste can hold, so a paste that never ends
// cannot take the memory of the process.
const maxPaste = 1 << 20

// decodePaste decodes a bracketed paste.
func decodePaste(buf []byte, more bool) (any, int) {
	body := buf[len(pasteStart):]
	i := strings.Index(string(body), pasteEnd)
	if i < 0 {
		if more && len(body) < maxPaste {
			return nil, 0
		}
		// Too long, or the end never came: what arrived is the paste, and
		// the rest of it is dropped rather than taken for keys. A start of
		// the end marker is left for the dropping to find the marker by.
		keep := markerStart(body)
		return Paste{Text: cleanPaste(string(body[:len(body)-keep])), cut: true}, len(buf) - keep
	}
	return Paste{Text: cleanPaste(string(body[:i]))}, len(pasteStart) + i + len(pasteEnd)
}

// markerStart is the length of the longest end of body that starts the end
// marker of a paste.
func markerStart(body []byte) int {
	for n := min(len(pasteEnd)-1, len(body)); n > 0; n-- {
		if strings.HasPrefix(pasteEnd, string(body[len(body)-n:])) {
			return n
		}
	}
	return 0
}

// cleanPaste makes line breaks "\n" and leaves out every control character
// but the tab, and the characters that reorder text on screen.
func cleanPaste(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case unicode.IsControl(r) || bidiControl(r):
			return -1
		}
		return r
	}, s)
}

// textRune reports whether r is text: a printable character, a space, or
// one of the joiners emoji and some scripts are made of. Controls and the
// characters that reorder text are not.
func textRune(r rune) bool {
	return unicode.IsPrint(r) || r == ' ' || r == 0x200d || r == 0x200c
}

// bidiControl reports whether r is one of the embeddings, overrides and
// isolates that reorder text on screen.
func bidiControl(r rune) bool {
	return (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069)
}
