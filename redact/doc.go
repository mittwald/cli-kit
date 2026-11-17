// Package redact removes credentials from text before it is logged, stored
// or shown.
//
// [Values] removes secrets a program knows, such as the token it just sent,
// as they are and in the forms they take inside a request: URL-escaped and
// JSON-escaped. It does not find a secret inside a larger encoded value,
// such as the Base64 string of HTTP Basic authentication.
//
// [Patterns] removes the credentials it recognizes by their shape, for text
// from elsewhere whose secrets are not known: labeled assignments such as
// password=…, Bearer tokens, and tokens whose prefix names their issuer.
// It is a safety net, not a substitute for [Values].
//
// Both replace what they find with [Marker]. The package depends on the
// standard library only, so a logger can redact without pulling in a
// keyring or a terminal.
package redact
