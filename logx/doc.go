// Package logx holds log/slog handlers for command-line tools.
//
// [LineHandler] writes each record as one plain line, the message and then
// its attributes as key=value, which is what a --verbose flag shows on the
// error stream. A log line often carries text from elsewhere, such as a
// server's error message or a request URL, so every line is made plain
// text (term.Plain), and credentials it recognizes by their shape are
// replaced (secret.RedactPatterns) before it is written.
//
// The handler lives in a package of its own because it needs both term and
// secret, and secret already depends on term.
package logx
