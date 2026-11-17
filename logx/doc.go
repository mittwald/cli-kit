// Package logx holds log/slog handlers for command-line tools.
//
// [LineHandler] writes each record as one plain line, the message and then
// its attributes as key=value, which is what a --verbose flag shows on the
// error stream. A log line often carries text from elsewhere, such as a
// server's error message or a request URL, so every line is made plain
// text (term.Plain), and credentials it recognizes by their shape are
// replaced (redact.Patterns) before it is written.
//
// The handler depends on term and redact only, so a tool that logs does not
// compile in the keyring and prompt code of the secret package.
package logx
