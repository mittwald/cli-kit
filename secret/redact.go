package secret

import "github.com/mittwald/cli-kit/redact"

// Redacted replaces a secret in text passed through [Redact].
//
// Deprecated: Use [redact.Marker].
const Redacted = redact.Marker

// Redact returns text with every occurrence of each secret replaced by
// [Redacted].
//
// Deprecated: Use [redact.Values], which does the same.
func Redact(text string, secrets ...string) string {
	return redact.Values(text, secrets...)
}

// RedactPatterns returns text with the credentials it can recognize by
// their shape replaced by [Redacted].
//
// Deprecated: Use [redact.Patterns], which does the same.
func RedactPatterns(text string) string {
	return redact.Patterns(text)
}
