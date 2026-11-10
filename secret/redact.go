package secret

import (
	"slices"
	"strings"
)

// Redacted replaces a secret in text passed through [Redact].
const Redacted = "[REDACTED]"

// Redact returns text with every occurrence of each secret replaced by
// [Redacted]. Use it on anything that may carry a secret before it is
// logged or shown: an HTTP dump, an error from a server that echoes its
// input. Empty secrets are ignored; longer secrets are replaced first, so a
// secret that contains another is removed whole.
func Redact(text string, secrets ...string) string {
	ordered := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if s != "" {
			ordered = append(ordered, s)
		}
	}
	slices.SortFunc(ordered, func(a, b string) int { return len(b) - len(a) })
	for _, s := range ordered {
		text = strings.ReplaceAll(text, s, Redacted)
	}
	return text
}
