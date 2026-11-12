package secret

import (
	"bytes"
	"cmp"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
)

// Redacted replaces a secret in text passed through [Redact].
const Redacted = "[REDACTED]"

// Redact returns text with every occurrence of each secret replaced by
// [Redacted]. Use it on anything that may carry a secret before it is
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
// Redact cannot find a secret that is part of a larger encoded value. The
// credentials of HTTP Basic authentication are the user name and the
// password encoded as one Base64 string, and a token inside a Base64 or
// compressed body looks like nothing Redact knows. Remove such headers and
// bodies before text reaches a log, or pass the encoded value as a secret
// of its own.
func Redact(text string, secrets ...string) string {
	type span struct{ start, end int }
	var spans []span
	seen := make(map[string]bool)
	for _, s := range secrets {
		for _, form := range forms(s) {
			if form == "" || seen[form] {
				continue
			}
			seen[form] = true
			// One byte on from each match, so that occurrences which overlap
			// themselves, such as "aa" in "aaa", are all found.
			for from := 0; ; {
				i := strings.Index(text[from:], form)
				if i < 0 {
					break
				}
				spans = append(spans, span{from + i, from + i + len(form)})
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
		b.WriteString(Redacted)
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

// forms returns secret and the escaped forms Redact looks for. It returns
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
