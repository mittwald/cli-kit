package format

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/printer"
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

// WriteYAML writes the view [WriteJSON] writes, as YAML. The value goes
// through its JSON encoding first, so the json tags stay the single source of
// truth for field names, omitted fields and the formatting custom
// MarshalJSON methods apply. The JSON is then decoded into an ordered map, so
// fields keep the order the struct defines instead of being sorted.
//
// Scalars whose plain YAML form would read back as another type (a time, a
// date, a boolean, a number, null) are quoted, and sequences are indented
// under their key. A string that spans lines is a literal block.
//
// The output carries no control character but the line breaks of its
// structure and of literal blocks, so it is as safe to print on a terminal
// as it is to parse. A string with any other control character — C0, DEL or
// C1, a tab or a carriage return as much as ESC or BEL — is written double
// quoted with that character escaped (\x1b, \a, \t, \u009b), and so are
// the line and paragraph separators U+2028 and U+2029, which YAML 1.1
// readers take for line breaks, and U+FFFE and U+FFFF, which YAML does not
// allow. Such a string reads back as the string it was.
//
// A string is double-quoted, too, where its plain form or a literal block
// would read back as something else: a key that spans lines; a string that
// spans lines but is made only of line breaks and spaces, starts its first
// line with text with a space, or ends its last line with one; .inf or .nan,
// which read back as numbers; a string starting with "? ", which reads back
// as a mapping; "<<" and "=", which YAML 1.1 reads as a merge and a default
// value; and, where it starts a line at the left margin as a key or the
// whole document, a string starting with "---" or "...", which mark
// documents, or with a byte order mark, which a reader drops.
func WriteYAML(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var document any
	if err := yaml.UnmarshalWithOptions(data, &document, yaml.UseOrderedMap()); err != nil {
		return err
	}
	node, err := yaml.ValueToNode(document, yaml.IndentSequence(true))
	if err != nil {
		return err
	}
	if root, ok := node.(*ast.StringNode); ok {
		quote(root, atRoot)
	}
	ast.Walk(quoter{}, node)
	var p printer.Printer
	_, err = w.Write(p.PrintNode(node))
	return err
}

// place is where a string stands in a document, which decides what it may
// hold unquoted.
type place int

const (
	// inValue is a value in a mapping or a sequence, after its key or dash.
	inValue place = iota
	// atRoot is the document's one node, at the start of the stream.
	atRoot
	// inKey is a mapping key: it starts a line and has to stay on it.
	inKey
)

// quoter double-quotes the strings of a document that cannot be written
// plain or as a literal block, as WriteYAML describes. A double-quoted string
// is written with strconv.Quote, whose escapes YAML reads the same way.
type quoter struct{}

// Visit quotes node when it is such a string, or the key of a mapping value
// that is one.
func (q quoter) Visit(node ast.Node) ast.Visitor {
	switch n := node.(type) {
	case *ast.MappingValueNode:
		if key, ok := n.Key.(*ast.StringNode); ok {
			quote(key, inKey)
		}
	case *ast.StringNode:
		quote(n, inValue)
	}
	return q
}

// quote turns a plain string into a double-quoted one when its value needs
// it where it stands. A string the encoder quoted already holds its escaped
// form, which carries no control character, and is left as it is.
func quote(n *ast.StringNode, at place) {
	if n.Token == nil || n.Token.Type == token.DoubleQuoteType || n.Token.Type == token.SingleQuoteType {
		return
	}
	if needsQuotes(n.Value, at) {
		n.Token.Type = token.DoubleQuoteType
	}
}

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
	if specialFloat(s) || strings.HasPrefix(s, "? ") || s == "<<" || s == "=" {
		return true
	}
	if at != inValue && (strings.HasPrefix(s, "---") || strings.HasPrefix(s, "...") || strings.HasPrefix(s, "\ufeff")) {
		return true
	}
	if !strings.Contains(s, "\n") {
		return false
	}
	return at == inKey || !literalSafe(s)
}

// specialFloat reports whether s is how YAML writes infinity or not a
// number, which the encoder leaves plain.
func specialFloat(s string) bool {
	switch strings.TrimLeft(s, "+-") {
	case ".inf", ".Inf", ".INF":
		return len(s) <= len(".inf")+1
	case ".nan", ".NaN", ".NAN":
		return len(s) == len(".nan")
	}
	return false
}

// literalSafe reports whether a literal block carries s, a string with a
// line break and no other control character, exactly. A reader takes the
// block's indentation from its first line with text, so that line and the
// empty ones before it must not start with a space, and there has to be such
// a line. The encoder trims spaces from the end of the block, so its last
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
