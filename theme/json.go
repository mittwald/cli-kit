package theme

import "strings"

// PaintJSON paints text, JSON as an encoder wrote it, compact or indented:
// the names of object members in JSONKey, string values in JSONString,
// numbers in JSONNumber, true and false in JSONBool and null in JSONNull.
// Quotes are painted with what they enclose; brackets, braces, commas,
// colons and white space stay plain. Paint with t.For(w) what is written to
// w, so it is painted only where w takes color:
//
//	data, _ := json.MarshalIndent(v, "", "  ")
//	fmt.Println(theme.Active().For(os.Stdout).PaintJSON(string(data)))
//
// PaintJSON does not parse: it reads tokens, and text that is not JSON comes
// out with its strings, numbers and literals painted as it finds them, and
// everything else as it is. A string is never painted past the end of its
// line, so a quote that is never closed does not color the lines after it.
//
// Like [Theme.PaintDiff], PaintJSON takes nothing out of the text. JSON
// written by encoding/json holds no escape sequence, since it escapes every
// control character in a string. JSON from elsewhere that was not decoded
// and encoded again goes through term.Plain first.
func (t Theme) PaintJSON(text string) string {
	if t.JSONKey == "" && t.JSONString == "" && t.JSONNumber == "" && t.JSONBool == "" && t.JSONNull == "" {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + len(text)/2)
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '"':
			end := stringEnd(text, i)
			style := t.JSONString
			if keyFollows(text, end) {
				style = t.JSONKey
			}
			b.WriteString(style.Paint(text[i:end]))
			i = end
		case c == '-' || isDigit(c) || isLetter(c):
			end := i + 1
			for end < len(text) && wordByte(text[end]) {
				end++
			}
			b.WriteString(t.jsonWordStyle(text[i:end]).Paint(text[i:end]))
			i = end
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

// jsonWordStyle is the style of a bare word: a literal, a number, or
// something that is neither and stays plain.
func (t Theme) jsonWordStyle(word string) Style {
	switch word {
	case "true", "false":
		return t.JSONBool
	case "null":
		return t.JSONNull
	}
	if jsonNumber(word) {
		return t.JSONNumber
	}
	return ""
}

// stringEnd returns the index after the string that starts with the quote
// at start: after its closing quote, or at the end of its line or of text
// when it is never closed.
func stringEnd(text string, start int) int {
	for i := start + 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			if i+1 < len(text) && text[i+1] != '\n' && text[i+1] != '\r' {
				i++
			}
		case '"':
			return i + 1
		case '\n', '\r':
			return i
		}
	}
	return len(text)
}

// keyFollows reports whether the next byte after white space at i is a
// colon, which makes the string before i the name of an object member.
func keyFollows(text string, i int) bool {
	for ; i < len(text); i++ {
		switch text[i] {
		case ' ', '\t', '\n', '\r':
			continue
		case ':':
			return true
		}
		return false
	}
	return false
}

// jsonNumber reports whether word is a number as JSON writes one:
// an optional minus, an integer without leading zeros, an optional
// fraction and an optional exponent.
func jsonNumber(word string) bool {
	i := 0
	if i < len(word) && word[i] == '-' {
		i++
	}
	switch {
	case i < len(word) && word[i] == '0':
		i++
	case i < len(word) && isDigit(word[i]):
		for i < len(word) && isDigit(word[i]) {
			i++
		}
	default:
		return false
	}
	if i < len(word) && word[i] == '.' {
		i++
		if i == len(word) || !isDigit(word[i]) {
			return false
		}
		for i < len(word) && isDigit(word[i]) {
			i++
		}
	}
	if i < len(word) && (word[i] == 'e' || word[i] == 'E') {
		i++
		if i < len(word) && (word[i] == '+' || word[i] == '-') {
			i++
		}
		if i == len(word) || !isDigit(word[i]) {
			return false
		}
		for i < len(word) && isDigit(word[i]) {
			i++
		}
	}
	return i == len(word)
}

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// wordByte reports whether c continues a bare word: a number with its sign,
// fraction and exponent, or a literal.
func wordByte(c byte) bool {
	return isDigit(c) || isLetter(c) || c == '.' || c == '+' || c == '-' || c == '_'
}
