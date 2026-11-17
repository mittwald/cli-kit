package redact

import (
	"bytes"
	"cmp"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
)

// Marker is what replaces a secret in text passed through [Values] or
// [Patterns].
const Marker = "[REDACTED]"

// Values returns text with every occurrence of each secret replaced by
// [Marker]. Use it on anything that may carry a secret before it is
// logged or shown: an HTTP dump, an error from a server that echoes its
// input. Empty secrets are ignored.
//
// A secret is found as it is and in the forms it takes on its way through
// a request: escaped for a URL query or a form (with a space as "+" or as
// "%20"), escaped for a URL path, and escaped for a JSON string. All
// occurrences are looked for in text as it was passed in and removed
// together, so secrets that overlap, or contain one another, leave no part
// of either behind.
//
// Values cannot find a secret that is part of a larger encoded value. The
// credentials of HTTP Basic authentication are the user name and the
// password encoded as one Base64 string, and a token inside a Base64 or
// compressed body looks like nothing Values knows. Remove such headers and
// bodies before text reaches a log, or pass the encoded value as a secret
// of its own.
func Values(text string, secrets ...string) string {
	type span struct{ start, end int }
	var spans []span
	seen := make(map[string]bool)
	for _, s := range secrets {
		for _, form := range forms(s) {
			if form == "" || seen[form] {
				continue
			}
			seen[form] = true
			find := finder(form)
			// One byte on from each match, so that occurrences which overlap
			// themselves, such as "aa" in "aaa", are all found.
			for from := 0; from < len(text); {
				i, n := find(text[from:])
				if i < 0 {
					break
				}
				spans = append(spans, span{from + i, from + i + n})
				from += i + 1
			}
		}
	}
	if len(spans) == 0 {
		return text
	}
	slices.SortFunc(spans, func(a, b span) int {
		return cmp.Or(cmp.Compare(a.start, b.start), cmp.Compare(a.end, b.end))
	})
	var b strings.Builder
	pos := 0
	current := spans[0]
	flush := func() {
		b.WriteString(text[pos:current.start])
		b.WriteString(Marker)
		pos = current.end
	}
	for _, next := range spans[1:] {
		if next.start < current.end {
			current.end = max(current.end, next.end)
			continue
		}
		flush()
		current = next
	}
	flush()
	b.WriteString(text[pos:])
	return b.String()
}

// finder returns the function that finds form in a text: the index of its
// first occurrence and its length, or -1. The hexadecimal digits of a URL
// escape (%2F) match in either case, as both are valid; every other byte
// matches exactly, so secrets that are not UTF-8 are found too.
func finder(form string) func(string) (int, int) {
	if !strings.Contains(form, "%") {
		return func(text string) (int, int) { return strings.Index(text, form), len(form) }
	}
	// hex marks the bytes of form that are digits of a URL escape.
	hex := make([]bool, len(form))
	for i := 0; i+2 < len(form); i++ {
		if form[i] == '%' && isHex(form[i+1]) && isHex(form[i+2]) {
			hex[i+1], hex[i+2] = true, true
			i += 2
		}
	}
	matchAt := func(text string, at int) bool {
		for j := range len(form) {
			c := text[at+j]
			if c == form[j] || hex[j] && lowerHex(c) == lowerHex(form[j]) {
				continue
			}
			return false
		}
		return true
	}
	return func(text string) (int, int) {
		for at := 0; at+len(form) <= len(text); at++ {
			if matchAt(text, at) {
				return at, len(form)
			}
		}
		return -1, 0
	}
}

// lowerHex returns c with an upper-case letter made lower case.
func lowerHex(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// isHex reports whether c is a hexadecimal digit.
func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// forms returns secret and the escaped forms Values looks for. It returns
// nothing for an empty secret.
func forms(secret string) []string {
	if secret == "" {
		return nil
	}
	query := url.QueryEscape(secret)
	return []string{
		secret,
		query,
		strings.ReplaceAll(query, "+", "%20"),
		url.PathEscape(secret),
		jsonEscape(secret, true),
		jsonEscape(secret, false),
	}
}

// jsonEscape returns s as it stands between the quotes of a JSON string,
// with <, > and & written as \u escapes, as encoding/json does by default,
// or as they are.
func jsonEscape(s string, escapeHTML bool) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(escapeHTML)
	if err := enc.Encode(s); err != nil {
		return ""
	}
	quoted := strings.TrimSuffix(buf.String(), "\n")
	return strings.TrimSuffix(strings.TrimPrefix(quoted, `"`), `"`)
}
