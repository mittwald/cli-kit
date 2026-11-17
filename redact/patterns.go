package redact

import (
	"regexp"
	"strings"
)

// The shapes [Patterns] looks for.
var (
	// labelPattern is the name of a credential and what assigns it a value,
	// in JSON, YAML, an environment file or a query: the name, quoted or
	// not, also in the backslash-escaped quotes of JSON inside a JSON
	// string (group 1 is the quote after it), then a colon or an equals
	// sign, with spaces and tabs around it but no line break. The value
	// that follows is read by labeledValue.
	labelPattern = regexp.MustCompile(`(?i)(?:\\?["'])?(?:(?:[a-z0-9]+[_-])*(?:api[_-]?key|access[_-]?token|token|secret|password|credential|authorization|auth))(\\?["'])?[ \t]*[:=][ \t]*`)
	// schemePattern is the scheme of an HTTP authorization at the start of
	// an unquoted value, which the credential after it belongs to.
	schemePattern = regexp.MustCompile(`(?i)^(?:Basic|Bearer|Bot|Digest|Negotiate|Token)[ \t]+`)
	// bearerPattern is the credential of an HTTP Bearer authorization.
	bearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{8,}`)
	// prefixedPattern is a token whose prefix names its issuer: OpenAI and
	// Anthropic (sk-), GitHub (ghp_, gho_, ghu_, ghs_, ghr_), GitLab
	// (glpat-) and an AWS access key ID (AKIA).
	prefixedPattern = regexp.MustCompile(`(?i)\b(?:sk-[A-Za-z0-9_-]{8,}|gh[pousr]_[A-Za-z0-9]{8,}|glpat-[A-Za-z0-9_-]{8,}|AKIA[A-Z0-9]{16,})`)
	// propertiesPattern is the tags and anchors of a YAML node in front of
	// its value: !!str, !secret, a verbatim !<tag:yaml.org,2002:str>, &name,
	// followed by white space or the end of their line, below which the
	// value may stand.
	propertiesPattern = regexp.MustCompile(`(?m)\A(?:(?:!<[^>\s]*>|[!&][^\s,\[\]{}]*)(?:[ \t]+|$))+`)
	// blockPattern is the header of a YAML block scalar, | or > with its
	// chomping and indentation indicators, up to the end of its line.
	blockPattern = regexp.MustCompile(`^[|>][-+0-9]*[ \t]*(?:#[^\n]*)?(?:\r?\n|$)`)
)

// Patterns returns text with the credentials it can recognize by
// their shape replaced by [Marker]. Use it where the secrets are not known
// in advance, as they are for [Values]: on what a server, a model or a log
// file says, before it is stored or shown. It replaces
//
//   - the value of a labeled assignment, in JSON, YAML, an environment
//     file or a query string, whose name ends in api_key, access_token,
//     token, secret, password, credential, authorization or auth, in any
//     case and with "-" or "_" between the words;
//   - the credential after "Bearer ";
//   - tokens whose prefix names their issuer: sk-, ghp_, gho_, ghu_, ghs_,
//     ghr_, glpat- and the AKIA of an AWS access key ID.
//
// The value of an assignment is read the way the formats write it, so that
// none of it is left behind and the text keeps its structure:
//
//   - A quoted value keeps its quotes: {"api_key":"sk-…"} becomes
//     {"api_key":"[REDACTED]"}. Backslash escapes and the doubled quotes
//     of YAML ('it”s') are part of the value.
//   - An unquoted value is replaced without quotes, DB_PASSWORD=hunter2
//     becomes DB_PASSWORD=[REDACTED], unless it is the value of a quoted
//     JSON name, {"token": 12345}, which becomes {"token": "[REDACTED]"} so
//     that JSON stays JSON. It runs up to white space, a comma, a
//     semicolon, a closing brace or a quote, so the rest of a query string
//     after a credential goes with it, and takes the scheme of an HTTP
//     authorization along: "Authorization: Basic …" becomes
//     "Authorization: [REDACTED]".
//   - An array or an object is replaced as a whole, nested ones and
//     brackets inside its strings included: {"token": ["a", "b"]} becomes
//     {"token": "[REDACTED]"}.
//   - An HTTP authorization with a scheme, such as "Authorization: Digest
//     username=…, response=…", is replaced up to the end of its line.
//   - A YAML block scalar, password: | or >, and a value on the lines
//     indented below its name, are replaced with all of those lines. A
//     name without a value on its line or below it, as in an empty
//     PASSWORD= followed by the next setting, is left alone.
//   - The value of a JSON name may stand on a later line. The tags and
//     anchors of a YAML value, password: !!str hunter2, go with it, and a
//     comment after the name stays while the value below it goes.
//   - An assignment inside a JSON string, as in
//     {"message":"token=abc def"}, is replaced inside the string, and its
//     value ends at the latest where the string does. A value in the
//     escaped quotes of JSON inside a JSON string keeps them.
//   - A quoted value, an array or an object that is never closed, in text
//     cut short, is replaced up to the end of the text, or of the JSON
//     string it is in.
//
// A secret without a label or a known prefix looks like any other word and
// stays. Patterns is a safety net for text from elsewhere, not a
// substitute for [Values] with the secrets a program holds itself.
func Patterns(text string) string {
	if text == "" {
		return text
	}
	text = redactLabeled(text)
	text = bearerPattern.ReplaceAllString(text, "Bearer "+Marker)
	return prefixedPattern.ReplaceAllString(text, Marker)
}

// redactLabeled replaces the value of every labeled assignment in text.
func redactLabeled(text string) string {
	var (
		b         strings.Builder
		inStrings jsonStrings
	)
	pos := 0
	for pos < len(text) {
		m := labelPattern.FindStringSubmatchIndex(text[pos:])
		if m == nil {
			break
		}
		for i := range m {
			if m[i] >= 0 {
				m[i] += pos
			}
		}
		start := m[1]
		jsonName := m[2] >= 0 && text[m[2]:m[3]] == `"`
		end, replacement := labeledValue(text, start, m[0], inStrings.inside(text, start), jsonName)
		b.WriteString(text[pos:start])
		if end == start {
			// No value: nothing to replace, and the label is passed by.
			pos = start
			continue
		}
		b.WriteString(replacement)
		pos = end
	}
	b.WriteString(text[pos:])
	return b.String()
}

// labeledValue reads the value that starts at start, after a label that
// starts at label, and returns where it ends and what replaces it. inString
// says whether the value lies inside a JSON string, jsonName whether the
// label is a quoted JSON name. A value of nothing ends at start.
//
// The value of a JSON name may follow on a later line, after JSON white
// space, which is kept. The tags and anchors of a YAML node in front of a
// value (!!str, &name) are replaced with it.
func labeledValue(text string, start, label int, inString, jsonName bool) (int, string) {
	from := start
	if jsonName && !inString {
		for from < len(text) && strings.IndexByte(" \t\r\n", text[from]) >= 0 {
			from++
		}
		// A quoted name is YAML as often as JSON. A value on a later line
		// that cannot start a JSON value, such as a YAML sequence or block,
		// is left to the YAML rules, which take all of it.
		if strings.ContainsRune(text[start:from], '\n') && (!jsonMember(text, label) || !jsonValueStart(text[from:])) {
			from = start
		}
	}
	properties := 0
	if !inString {
		if loc := propertiesPattern.FindStringIndex(text[from:]); loc != nil && loc[0] == 0 {
			properties = loc[1]
		}
	}
	comment := ""
	if at := from + properties; !inString && at > 0 && (text[at-1] == ' ' || text[at-1] == '\t') &&
		strings.HasPrefix(text[at:], "#") {
		// A YAML comment ends the line; the value may stand below it.
		comment = text[at:]
		if i := strings.IndexByte(comment, '\n'); i >= 0 {
			comment = strings.TrimSuffix(comment[:i], "\r")
		}
	}
	at := from + properties + len(comment)
	end, replacement := valueAt(text, at, label, inString, jsonName)
	if end == at {
		return start, ""
	}
	return end, text[start:from] + comment + replacement
}

// valueAt is labeledValue for a value that starts right at start.
func valueAt(text string, start, label int, inString, jsonName bool) (int, string) {
	rest := text[start:]
	switch {
	case rest == "":
		return start, ""
	case inString && rest[0] == '"' && closesString(text, start):
		// No value before the end of the JSON string the assignment is in.
		return start, ""
	case inString && rest[0] == '"':
		// A quote that does not end the string: what looked like a JSON
		// string was a stray quote, as in the YAML {note: 5" screen,
		// password: "…"}, and the value is a quoted string of its own.
		return quotedEnd(text, start, false), `"` + Marker + `"`
	case strings.HasPrefix(rest, `\"`):
		return escapedStringEnd(text, start), `\"` + Marker + `\"`
	case rest[0] == '"' || rest[0] == '\'':
		return quotedEnd(text, start, inString), rest[:1] + Marker + rest[:1]
	case rest[0] == '[' || rest[0] == '{':
		return compositeEnd(text, start, inString), quoted(Marker, jsonName)
	case !inString && (rest[0] == '\n' || strings.HasPrefix(rest, "\r\n")):
		// Nothing on the line of the label: the value is on the lines
		// indented below it, as YAML may write it, or there is none, as in
		// an empty PASSWORD= followed by the next setting.
		from := start + 1
		if rest[0] == '\r' {
			from++
		}
		end := blockEnd(text, from, lineIndent(text, label))
		if end == from {
			// YAML lets a sequence stand at the indentation of its key.
			end = sequenceEnd(text, from, lineIndent(text, label))
		}
		if end == from {
			return start, ""
		}
		// The value keeps the line and indentation of its first line that
		// is not blank.
		value := text[from:end]
		for {
			line, rest, more := strings.Cut(value, "\n")
			if !more || strings.TrimSpace(line) != "" {
				break
			}
			value = rest
		}
		indent := value[:len(value)-len(strings.TrimLeft(value, " \t"))]
		return end, text[start:from] + indent + Marker
	}
	if header := blockPattern.FindString(rest); header != "" && !inString {
		from := start + len(header)
		end := blockEnd(text, from, lineIndent(text, label))
		if end == from {
			// No lines below the header: its line break stays.
			return end, Marker + lineBreakOf(header)
		}
		// The line break after the last line of the block stays.
		return end, Marker
	}
	scheme := schemePattern.FindString(rest)
	if scheme != "" {
		if end := authorizationEnd(text, start+len(scheme), inString); end > start+len(scheme) {
			return end, quoted(Marker, jsonName)
		}
	}
	// A member of a flow mapping, YAML ({password: two words}) or JSON,
	// ends its plain value at a comma or a closing bracket, not at a space
	// or a line break: a YAML flow scalar may have both, and a JSON number
	// or literal ends there anyway.
	flow := !inString && jsonMember(text, label)
	end := start + len(scheme)
	for end < len(text) {
		c := text[end]
		if c == '\\' {
			// An escape, \" inside a JSON string among them, is part of
			// the value.
			end = min(end+2, len(text))
			continue
		}
		if flow {
			if strings.IndexByte(`,}]`, c) >= 0 {
				break
			}
		} else if c <= ' ' || strings.IndexByte(`,;}"'`, c) >= 0 {
			break
		}
		end++
	}
	if flow {
		end = start + len(strings.TrimRight(text[start:end], " \t\r\n"))
	}
	if end == start+len(scheme) {
		// Nothing after a scheme: the scheme word is the value, as in
		// auth=Basic. Nothing at all: no value.
		end = start + len(strings.TrimRight(scheme, " \t"))
		if end == start {
			return start, ""
		}
	}
	if !inString && !flow && ownLine(text, label) {
		end = max(end, plainScalarEnd(text, start, lineIndent(text, label)))
	}
	return end, quoted(Marker, jsonName)
}

// ownLine reports whether the label at label starts its line, white space
// and YAML sequence dashes aside, as a YAML key or a setting in a file of
// settings does.
func ownLine(text string, label int) bool {
	lineStart := strings.LastIndexByte(text[:label], '\n') + 1
	return lineStart+lineIndent(text, label) == label
}

// plainScalarEnd returns where a value that starts at start beside a label
// on its own line ends, read as a YAML plain scalar: at the end of the line
// or before a comment (" #") on it, and past the lines below that are
// indented further than indent, the indentation of the label's line, which
// continue it.
func plainScalarEnd(text string, start, indent int) int {
	lineEnd := len(text)
	if i := strings.IndexByte(text[start:], '\n'); i >= 0 {
		lineEnd = start + i
	}
	line := text[start:lineEnd]
	if i := strings.Index(line, " #"); i >= 0 {
		line = line[:i]
	} else if i := strings.Index(line, "\t#"); i >= 0 {
		line = line[:i]
	}
	end := start + len(strings.TrimRight(line, " \t\r"))
	if lineEnd < len(text) && len(line) == lineEnd-start {
		if more := blockEnd(text, lineEnd+1, indent); more > lineEnd+1 {
			end = more
		}
	}
	return end
}

// closesString reports whether the quote at i reads as the end of a JSON
// string: what follows it, after white space, is a comma, a closing bracket
// or brace, a colon, or the end of the line or of the text.
func closesString(text string, i int) bool {
	for i++; i < len(text); i++ {
		switch text[i] {
		case ' ', '\t', '\r':
			continue
		case ',', '}', ']', ':', '\n':
			return true
		}
		return false
	}
	return true
}

// authorizationEnd returns where the credentials of an HTTP authorization
// that start at start end: at the end of the line, or inside a JSON string
// (inString) where that string ends, if that comes first. The parameters of
// a scheme such as Digest, username="…", nonce="…", response="…", are all
// part of them.
func authorizationEnd(text string, start int, inString bool) int {
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '\\':
			i++
		case '\n':
			return i - boolToInt(i > start && text[i-1] == '\r')
		case '"':
			if inString {
				return i
			}
		}
	}
	return len(text)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// quotedEnd returns the index after the string in double or single quotes
// that starts at start. A string that is never closed ends with the text,
// or inside a JSON string (inString) where that string does.
//
// In double quotes a backslash escapes the character after it. Single
// quotes are read both ways they are written: with a backslash as an
// escape, as in Python or JavaScript, and, as in YAML and the shell, with a
// backslash as itself and a doubled quote standing for one. The string
// ends where the later of the two readings ends it, so that no reading
// leaves a part of it behind.
func quotedEnd(text string, start int, inString bool) int {
	if text[start] == '\'' {
		return max(stringEnd(text, start, inString, true, false), stringEnd(text, start, inString, false, true))
	}
	return stringEnd(text, start, inString, true, false)
}

// stringEnd is quotedEnd for one way of reading the string: escapes says
// whether a backslash escapes the character after it, doubled whether a
// doubled quote stands for one.
func stringEnd(text string, start int, inString, escapes, doubled bool) int {
	quote := text[start]
	for i := start + 1; i < len(text); i++ {
		switch c := text[i]; {
		case c == '\\' && (escapes || inString):
			// Inside a JSON string a backslash escapes for that string,
			// whatever the inner string makes of it: \" is no end there.
			i++
		case c == quote && doubled && i+1 < len(text) && text[i+1] == quote:
			i++
		case c == quote:
			return i + 1
		case c == '"' && inString:
			return i
		}
	}
	return len(text)
}

// escapedStringEnd returns the index after the string in the escaped quotes
// \" of JSON inside a JSON string that starts at start. Its own escaped
// quotes, \\\", are part of it. A string that is never closed ends where
// the enclosing JSON string does, or with the text.
func escapedStringEnd(text string, start int) int {
	for i := start + 2; i < len(text); i++ {
		switch text[i] {
		case '"':
			return i
		case '\\':
			if i+1 < len(text) && text[i+1] == '"' {
				return i + 2
			}
			if i+1 < len(text) && text[i+1] == '\\' {
				// An escaped backslash of the enclosing string. Followed by
				// \", it escapes a quote of the inner string, which is part
				// of it; otherwise the character after it is read anew, so
				// a quote there still ends the inner string.
				if i+3 < len(text) && text[i+2] == '\\' && text[i+3] == '"' {
					i += 3
				} else {
					i++
				}
				continue
			}
			i++
		}
	}
	return len(text)
}

// blockEnd returns where the lines of a YAML block scalar end: before the
// line break ahead of the first line that is not blank and is indented no
// further than indent, the indentation of the line holding its key, or at
// the end of the text. from is where its first line starts.
func blockEnd(text string, from, indent int) int {
	end := from
	for line := from; line < len(text); {
		next := strings.IndexByte(text[line:], '\n')
		body := text[line:]
		if next >= 0 {
			body = text[line : line+next]
		}
		content := strings.TrimLeft(body, " \t")
		if strings.TrimSpace(content) != "" && len(body)-len(content) <= indent {
			break
		}
		end = line + len(strings.TrimRight(body, "\r"))
		if next < 0 {
			return len(text)
		}
		line += next + 1
	}
	return end
}

// sequenceEnd returns where a YAML sequence written at the indentation of its
// key ends: before the line break ahead of the first line that is not blank,
// is indented no further than indent and is not an item ("- …") at indent
// itself. from is where its first line starts; when that line is no item,
// there is no such sequence and sequenceEnd returns from.
func sequenceEnd(text string, from, indent int) int {
	end := from
	for line := from; line < len(text); {
		next := strings.IndexByte(text[line:], '\n')
		body := text[line:]
		if next >= 0 {
			body = text[line : line+next]
		}
		content := strings.TrimLeft(body, " \t")
		if trimmed := strings.TrimSpace(content); trimmed != "" && len(body)-len(content) <= indent {
			item := len(body)-len(content) == indent && (trimmed == "-" || strings.HasPrefix(content, "- ") || strings.HasPrefix(content, "-\t"))
			if !item {
				break
			}
		} else if end == from && trimmed != "" {
			// A first line indented further is a block, not this sequence.
			return from
		}
		end = line + len(strings.TrimRight(body, "\r"))
		if next < 0 {
			return len(text)
		}
		line += next + 1
	}
	return end
}

// lineIndent returns how far the line holding text[i] is indented, up to
// the key or setting that starts there.
func lineIndent(text string, i int) int {
	lineStart := strings.LastIndexByte(text[:i], '\n') + 1
	line := text[lineStart:i]
	rest := strings.TrimLeft(line, " \t")
	// A key in a sequence item is indented as far as the key itself: the
	// dashes in front of it are part of its indentation, as YAML counts
	// it, so the item's other keys are its siblings.
	for len(rest) > 1 && rest[0] == '-' && (rest[1] == ' ' || rest[1] == '\t') {
		rest = strings.TrimLeft(rest[1:], " \t")
	}
	return len(line) - len(rest)
}

// lineBreakOf returns the line break a block scalar header ends in.
func lineBreakOf(header string) string {
	switch {
	case strings.HasSuffix(header, "\r\n"):
		return "\r\n"
	case strings.HasSuffix(header, "\n"):
		return "\n"
	}
	return ""
}

// quoted returns s in double quotes when quote is set.
func quoted(s string, quote bool) string {
	if quote {
		return `"` + s + `"`
	}
	return s
}

// jsonStrings tells whether a position of a text lies inside a JSON string,
// reading the text once from its start as positions are asked for in
// increasing order.
//
// A double quote opens or closes a JSON string, except inside single
// quotes, the strings of YAML and the shell, where it is a character like
// any other. Every line starts outside of any string: a JSON string holds
// no line break, and so a stray quote, in prose or a plain YAML value, is
// forgotten at the end of its line.
type jsonStrings struct {
	pos    int
	in     bool
	single bool
}

// inside reports whether text[i] lies inside a string in double quotes.
func (s *jsonStrings) inside(text string, i int) bool {
	for ; s.pos < i && s.pos < len(text); s.pos++ {
		switch c := text[s.pos]; {
		case c == '\n':
			s.in, s.single = false, false
		case s.single:
			if c == '\'' {
				s.single = false
			}
		case c == '\\':
			if s.in {
				s.pos++
			}
		case c == '"':
			s.in = !s.in
		case c == '\'' && !s.in:
			s.single = true
		}
	}
	return s.in
}

// compositeEnd returns the index after the array or object that opens at
// start, counting nested brackets and braces and skipping over strings in
// double or single quotes, or len(text) when it is never closed. Inside a
// JSON string (inString), its strings are in escaped quotes, and it ends at
// the latest before the quote that ends the enclosing string. Strings in
// single quotes are read both ways quotedEnd reads them, and the later end
// wins.
func compositeEnd(text string, start int, inString bool) int {
	if inString {
		return escapedCompositeEnd(text, start)
	}
	return max(bracketEnd(text, start, false), bracketEnd(text, start, true))
}

// bracketEnd is compositeEnd outside a JSON string for one way of reading
// strings in single quotes: literal says whether a backslash in them is
// itself, with a doubled quote standing for one, or escapes the character
// after it. In double quotes a backslash always escapes.
func bracketEnd(text string, start int, literal bool) int {
	depth := 0
	var quote byte
	for i := start; i < len(text); i++ {
		c := text[i]
		switch {
		case quote == '\'' && literal:
			if c == '\'' {
				if i+1 < len(text) && text[i+1] == '\'' {
					i++
				} else {
					quote = 0
				}
			}
		case quote != 0:
			switch c {
			case '\\':
				i++
			case quote:
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(text)
}

// escapedCompositeEnd is compositeEnd for an array or object inside a JSON
// string, whose own strings are in the escaped quotes \".
func escapedCompositeEnd(text string, start int) int {
	depth := 0
	inner, single := false, false
	for i := start; i < len(text); i++ {
		switch c := text[i]; {
		case c == '"':
			return i
		case c == '\\' && i+1 < len(text) && text[i+1] == '\\':
			// An escaped backslash of the enclosing string. Followed by \",
			// it escapes a quote of the inner text: no delimiter. Otherwise
			// the character after it is read anew.
			if i+3 < len(text) && text[i+2] == '\\' && text[i+3] == '"' {
				i += 3
			} else {
				i++
			}
		case c == '\\':
			if i+1 < len(text) && text[i+1] == '"' && !single {
				inner = !inner
			}
			i++
		case inner:
		case c == '\'':
			// Brackets in a single-quoted string are text, as in a
			// composite outside a JSON string.
			single = !single
		case single:
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(text)
}

// jsonMember reports whether the name at label is a member of a JSON object:
// what stands before it, white space aside, opens the object or ends the
// member before it.
func jsonMember(text string, label int) bool {
	before := strings.TrimRight(text[:label], " \t\r\n")
	return before != "" && (before[len(before)-1] == '{' || before[len(before)-1] == ',')
}

// jsonValueStart reports whether s starts with what can start a JSON value:
// a string, an object, an array, a number, true, false or null.
func jsonValueStart(s string) bool {
	if s == "" {
		return false
	}
	switch c := s[0]; {
	case c == '"' || c == '{' || c == '[':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '-':
		return len(s) > 1 && s[1] >= '0' && s[1] <= '9'
	}
	for _, word := range []string{"true", "false", "null"} {
		if strings.HasPrefix(s, word) {
			return true
		}
	}
	return false
}
