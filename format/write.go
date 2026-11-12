package format

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/goccy/go-yaml/token"
)

// ErrNotStructured is returned by [Write] for a format other than JSON or
// YAML.
var ErrNotStructured = errors.New("format is not structured")

// Write writes v in format f, which must be [JSON] or [YAML].
func Write(w io.Writer, f Format, v any) error {
	switch f {
	case JSON:
		return WriteJSON(w, v)
	case YAML:
		return WriteYAML(w, v)
	}
	return fmt.Errorf("%w: %q", ErrNotStructured, f)
}

// WriteJSON writes v as JSON indented by two spaces, followed by a newline.
// Unlike json.MarshalIndent it leaves <, > and & as they are: the output is
// read in a terminal or by a program, not embedded in HTML.
//
// The output carries no control character but the newlines between its
// lines, so it is as safe to print on a terminal as it is to parse: besides
// the C0 controls, which encoding/json escapes, DEL and the C1 controls
// (U+007F–U+009F) are written as \u escapes too. A terminal takes U+009B for
// ESC [ and U+009D for ESC ], and encoding/json leaves both raw. Every string
// still decodes to the value it was.
func WriteJSON(w io.Writer, v any) error {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		return err
	}
	_, err := w.Write(escapeJSONControls(buf.Bytes()))
	return err
}

// delOrC1 reports whether r is DEL or a C1 control, the control characters
// encoding/json writes raw.
func delOrC1(r rune) bool { return r >= 0x7f && r <= 0x9f }

// escapeJSONControls writes DEL and the C1 controls in data, a document
// encoding/json wrote, as \u escapes. Outside its strings JSON is ASCII
// punctuation, digits and literals, so they occur only inside strings, where
// an escape stands for the same character.
func escapeJSONControls(data []byte) []byte {
	if !bytes.ContainsFunc(data, delOrC1) {
		return data
	}
	out := make([]byte, 0, len(data)+32)
	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		if delOrC1(r) {
			out = fmt.Appendf(out, `\u%04x`, r)
		} else {
			out = append(out, data[i:i+size]...)
		}
		i += size
	}
	return out
}

// WriteYAML writes the view [WriteJSON] writes, as block-style YAML. The
// value goes through its JSON encoding first, so the json tags stay the single
// source of truth for field names, omitted fields and the formatting custom
// MarshalJSON methods apply. Fields keep the order the struct defines instead
// of being sorted, and sequences are indented under their key.
//
// Every value has the type it has in JSON, for a YAML 1.1 reader as much as
// for a YAML 1.2 one:
//
//   - A number is written with the digits JSON has, never rounded: an integer
//     as it stands, however large, and a number with a fraction or an exponent
//     with a dot in its mantissa and a sign in its exponent (1e-7 as 1.0e-7,
//     1e21 as 1.0e+21), without which YAML 1.1 reads a string.
//   - A string is double-quoted where its plain form would read back as
//     something else for either version: a number in any spelling (0815,
//     1e3, 1_000, 0x1F, 0o17, 0b11, +1, .5, 1,000, .inf, .nan), a base 60
//     number or time (12:30:45), a date or a time stamp, a boolean or null
//     (true, yes, on, n, ~ and their other cases), a lone "?" or a string
//     starting with "? ", which read as a mapping key, and "<<" and "=", which
//     YAML 1.1 reads as a merge and a default value. So is a string that
//     starts or ends with a space, or holds what YAML reads as structure or a
//     comment.
//   - A string that spans lines is a literal block. It is double-quoted
//     instead where a block would read back as something else: as a key; made
//     only of line breaks and spaces; starting its first line with text with a
//     space; or ending its last line with one.
//   - Where it starts a line at the left margin, as a key or the whole
//     document, a string starting with "---" or "...", which mark documents,
//     or with a byte order mark, which a reader drops, is double-quoted.
//
// A key holding "<<" is double-quoted, and a key of more than 1000 bytes,
// around where readers stop looking for the colon of "key: value", is written
// as an explicit key: "? key" and ": value" on lines of their own. Where an
// object holds a key more than once, which only a custom MarshalJSON can
// produce, the key is written once, in its first place and with its last
// value, as JSON readers take it.
//
// The output carries no control character but the line breaks of its
// structure and of literal blocks, so it is as safe to print on a terminal
// as it is to parse. A string with any other control character — C0, DEL or
// C1, a tab or a carriage return as much as ESC or BEL — is written double
// quoted with that character escaped (\x1b, \a, \t, \u009b), and so are
// the line and paragraph separators U+2028 and U+2029, which YAML 1.1
// readers take for line breaks, and U+FFFE and U+FFFF, which YAML does not
// allow. Such a string reads back as the string it was.
func WriteYAML(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	document, err := decodeYAMLValue(decoder)
	if err != nil {
		return err
	}
	var e yamlEncoder
	e.document(document)
	_, err = w.Write(e.out)
	return err
}

// yamlMapping is a JSON object with its members in the order JSON has them.
type yamlMapping []yamlMember

// yamlMember is one member of a JSON object.
type yamlMember struct {
	key   string
	value any
}

// decodeYAMLValue reads the next JSON value from decoder as the document
// WriteYAML writes: nil, a bool, a json.Number, a string, a []any or a
// yamlMapping. Numbers keep the text JSON has, and objects their order. A
// member whose key came before replaces the value of the first one, which is
// what a JSON reader makes of an object that repeats a key.
func decodeYAMLValue(decoder *json.Decoder) (any, error) {
	tok, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	if delim == '[' {
		sequence := []any{}
		for decoder.More() {
			item, err := decodeYAMLValue(decoder)
			if err != nil {
				return nil, err
			}
			sequence = append(sequence, item)
		}
		_, err := decoder.Token()
		return sequence, err
	}
	mapping := yamlMapping{}
	var seen map[string]int
	for decoder.More() {
		tok, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("format: JSON object key is %T, not a string", tok)
		}
		value, err := decodeYAMLValue(decoder)
		if err != nil {
			return nil, err
		}
		if seen == nil {
			seen = make(map[string]int)
		}
		if i, repeated := seen[key]; repeated {
			mapping[i].value = value
			continue
		}
		seen[key] = len(mapping)
		mapping = append(mapping, yamlMember{key: key, value: value})
	}
	_, err = decoder.Token()
	return mapping, err
}

// yamlIndent is how far a nested block is indented under its key or dash.
const yamlIndent = 2

// maxSimpleKey is the longest key written as "key: value", in bytes as it is
// written. Readers built on libyaml give up on a key that does not end within
// 1024 characters; longer keys are written as explicit keys, which have no
// limit.
const maxSimpleKey = 1000

// yamlEncoder writes a document decodeYAMLValue read as block-style YAML.
type yamlEncoder struct {
	out []byte
}

// document writes the whole document. A sequence at the root is indented
// like one under a key.
func (e *yamlEncoder) document(v any) {
	switch v := v.(type) {
	case yamlMapping:
		if len(v) > 0 {
			e.mapping(v, 0, false)
			return
		}
	case []any:
		if len(v) > 0 {
			e.sequence(v, yamlIndent, false)
			return
		}
	}
	e.scalar(v, 0, atRoot)
	e.out = append(e.out, '\n')
}

// indent starts a line at column.
func (e *yamlEncoder) indent(column int) {
	for range column {
		e.out = append(e.out, ' ')
	}
}

// mapping writes the members of m as lines starting at column. With inline
// set the first member continues the line it is on, after the dash of a
// sequence.
func (e *yamlEncoder) mapping(m yamlMapping, column int, inline bool) {
	for i, member := range m {
		if i > 0 || !inline {
			e.indent(column)
		}
		start := len(e.out)
		e.string(member.key, column, inKey)
		if len(e.out)-start > maxSimpleKey {
			key := string(e.out[start:])
			e.out = append(e.out[:start], "? "...)
			e.out = append(e.out, key...)
			e.out = append(e.out, '\n')
			e.indent(column)
		}
		e.out = append(e.out, ':')
		e.nested(member.value, column)
	}
}

// nested writes v after the colon of a key at column: a block on the lines
// below, indented, or a scalar on the same line.
func (e *yamlEncoder) nested(v any, column int) {
	switch v := v.(type) {
	case yamlMapping:
		if len(v) > 0 {
			e.out = append(e.out, '\n')
			e.mapping(v, column+yamlIndent, false)
			return
		}
	case []any:
		if len(v) > 0 {
			e.out = append(e.out, '\n')
			e.sequence(v, column+yamlIndent, false)
			return
		}
	}
	e.out = append(e.out, ' ')
	e.scalar(v, column, inValue)
	e.out = append(e.out, '\n')
}

// sequence writes the items of s, their dashes at column. With inline set
// the first dash continues the line it is on, after the dash of the sequence
// s is an item of.
func (e *yamlEncoder) sequence(s []any, column int, inline bool) {
	for i, item := range s {
		if i > 0 || !inline {
			e.indent(column)
		}
		e.out = append(e.out, "- "...)
		switch item := item.(type) {
		case yamlMapping:
			if len(item) > 0 {
				e.mapping(item, column+yamlIndent, true)
				continue
			}
		case []any:
			if len(item) > 0 {
				e.sequence(item, column+yamlIndent, true)
				continue
			}
		}
		e.scalar(item, column, inItem)
		e.out = append(e.out, '\n')
	}
}

// scalar writes v, anything but a mapping or a sequence with members, without
// the line break after it. column is where the key or dash v belongs to
// starts, which a literal block is indented from.
func (e *yamlEncoder) scalar(v any, column int, at place) {
	switch v := v.(type) {
	case nil:
		e.out = append(e.out, "null"...)
	case bool:
		e.out = strconv.AppendBool(e.out, v)
	case json.Number:
		e.out = append(e.out, yamlNumber(v.String())...)
	case string:
		e.string(v, column, at)
	case yamlMapping:
		e.out = append(e.out, "{}"...)
	case []any:
		e.out = append(e.out, "[]"...)
	}
}

// string writes s plain, as a literal block or double-quoted, as where it
// stands allows. A double-quoted string is written with strconv.Quote, whose
// escapes YAML reads the same way.
func (e *yamlEncoder) string(s string, column int, at place) {
	switch {
	case needsQuotes(s, at):
		e.out = strconv.AppendQuote(e.out, s)
	case strings.Contains(s, "\n"):
		// The block ends with the line break of its last line, which the
		// caller writes. A string that ends in one keeps it ("|"), and one
		// that ends in more keeps them all ("|+").
		header, text := "|-", s
		if body, ok := strings.CutSuffix(s, "\n"); ok {
			header, text = "|", body
			if strings.HasSuffix(body, "\n") {
				header = "|+"
			}
		}
		e.out = append(e.out, header...)
		// An empty line carries the block's indentation under a key, unless
		// it is the last one, and none under a dash. Readers take either for
		// an empty line; they are written as this package always wrote them,
		// so output that was right before does not change.
		lines := strings.Split(text, "\n")
		for i, line := range lines {
			e.out = append(e.out, '\n')
			if line != "" || (at != inItem && i < len(lines)-1) {
				e.indent(column + yamlIndent)
				e.out = append(e.out, line...)
			}
		}
	default:
		e.out = append(e.out, s...)
	}
}

// yamlNumber returns the JSON number n as YAML 1.1 and YAML 1.2 both read
// it: an integer as it stands, and a number with a fraction or an exponent
// with a dot in its mantissa and a sign in its exponent, which YAML 1.1
// requires of a float.
func yamlNumber(n string) string {
	exponent := strings.IndexAny(n, "eE")
	if exponent < 0 {
		return n
	}
	mantissa, rest := n[:exponent], n[exponent+1:]
	if !strings.Contains(mantissa, ".") {
		mantissa += ".0"
	}
	if !strings.HasPrefix(rest, "+") && !strings.HasPrefix(rest, "-") {
		rest = "+" + rest
	}
	return mantissa + n[exponent:exponent+1] + rest
}

// place is where a string stands in a document, which decides what it may
// hold unquoted.
type place int

const (
	// inValue is a value in a mapping, after its key.
	inValue place = iota
	// inItem is an item of a sequence, after its dash.
	inItem
	// atRoot is the document's one node, at the start of the stream.
	atRoot
	// inKey is a mapping key: it starts a line and has to stay on it.
	inKey
)

// needsQuotes reports whether s, standing at at, has to be double-quoted:
// it has a character YAML cannot carry raw or a reader would take for a line
// break, or written plain or as a literal block it would read back as
// something else.
func needsQuotes(s string, at place) bool {
	for _, r := range s {
		switch {
		case r == '\n':
		case r < 0x20, delOrC1(r), r == '\u2028', r == '\u2029', r == '\ufffe', r == '\uffff':
			return true
		}
	}
	// What YAML 1.2 reads as something other than a string, structure and
	// the YAML 1.1 booleans included, and then what only some readers do.
	if token.IsNeedQuoted(s) || numberLike(s) || specialFloat(s) || dateLike(s) {
		return true
	}
	if s == "?" || strings.HasPrefix(s, "? ") || s == "<<" || s == "=" {
		return true
	}
	if (at == atRoot || at == inKey) && (strings.HasPrefix(s, "---") || strings.HasPrefix(s, "...") || strings.HasPrefix(s, "\ufeff")) {
		return true
	}
	if at == inKey && strings.Contains(s, "<<") {
		return true
	}
	if !strings.Contains(s, "\n") {
		return false
	}
	return at == inKey || !literalSafe(s)
}

// unsigned returns s without the sign it starts with, if any.
func unsigned(s string) string {
	if s != "" && (s[0] == '+' || s[0] == '-') {
		return s[1:]
	}
	return s
}

// specialFloat reports whether s is how YAML writes infinity or not a
// number, in any case and with or without a sign.
func specialFloat(s string) bool {
	s = unsigned(s)
	return strings.EqualFold(s, ".inf") || strings.EqualFold(s, ".nan")
}

// numberLike reports whether some YAML reader takes the plain scalar s for a
// number. It accepts every integer and float of YAML 1.1 and of the YAML 1.2
// core schema, and what readers accept beyond them: a decimal number with
// leading zeros (0815, a float for some and octal for others), an exponent
// without a dot (1e3), digits grouped by underscores or commas (1_000,
// 1,000), hexadecimal, octal and binary numbers (0x1F, 0o17, 0b11), and
// base 60 numbers (12:30:45, 1:30.5). It asks for less than any one reader
// does, so that a string no reader takes for a number may be quoted, but
// none that one does stays plain.
func numberLike(s string) bool {
	s = unsigned(s)
	if len(s) > 2 && s[0] == '0' {
		switch s[1] | 0x20 {
		case 'x':
			return only(s[2:], "0123456789abcdefABCDEF_")
		case 'o':
			return only(s[2:], "01234567_")
		case 'b':
			return only(s[2:], "01_")
		}
	}
	// The integer part: digits and their separators. Only a fraction may
	// stand without one (.5).
	i := 0
	for i < len(s) && (isDigit(s[i]) || (i > 0 && (s[i] == '_' || s[i] == ','))) {
		i++
	}
	if i == 0 && (len(s) < 2 || s[0] != '.' || !isDigit(s[1])) {
		return false
	}
	// Base 60 digits.
	for i+1 < len(s) && s[i] == ':' && isDigit(s[i+1]) {
		i++
		for i < len(s) && isDigit(s[i]) {
			i++
		}
	}
	// The fraction.
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && (isDigit(s[i]) || s[i] == '_') {
			i++
		}
	}
	// The exponent.
	if i < len(s) && s[i]|0x20 == 'e' {
		digits := unsigned(s[i+1:])
		return digits != "" && only(digits, "0123456789")
	}
	return i == len(s)
}

// dateLike reports whether s starts like a date some YAML reader takes for
// a time stamp: a year of four digits, a month and a day of one or two, and
// then nothing, or a "T" or white space and the digit a time starts with.
// Readers differ in how much of the rest they check; quoting a string that
// only looks like a date costs nothing.
func dateLike(s string) bool {
	if len(s) < len("2006-1-2") || !only(s[:4], "0123456789") || s[4] != '-' {
		return false
	}
	i := 5
	for range 2 {
		start := i
		for i < len(s) && i < start+2 && isDigit(s[i]) {
			i++
		}
		if i == start {
			return false
		}
		if start == 5 {
			if i == len(s) || s[i] != '-' {
				return false
			}
			i++
		}
	}
	if i == len(s) {
		return true
	}
	rest := strings.TrimLeft(s[i:], " \t")
	if len(rest) == len(s[i:]) {
		if s[i]|0x20 != 't' {
			return false
		}
		rest = rest[1:]
	}
	return rest != "" && isDigit(rest[0])
}

// isDigit reports whether c is an ASCII digit.
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// only reports whether s is made of the bytes in set alone. An empty s is.
func only(s, set string) bool {
	for i := range len(s) {
		if strings.IndexByte(set, s[i]) < 0 {
			return false
		}
	}
	return true
}

// literalSafe reports whether a literal block carries s, a string with a
// line break and no other control character, exactly. A reader takes the
// block's indentation from its first line with text, so that line and the
// empty ones before it must not start with a space, and there has to be such
// a line. A reader may drop spaces from the end of the block, so its last
// line must not end with one either.
func literalSafe(s string) bool {
	if strings.HasSuffix(strings.TrimSuffix(s, "\n"), " ") {
		return false
	}
	for line := range strings.SplitSeq(s, "\n") {
		if strings.HasPrefix(line, " ") {
			return false
		}
		if line != "" {
			return true
		}
	}
	return false
}
