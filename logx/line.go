package logx

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/mittwald/cli-kit/secret"
	"github.com/mittwald/cli-kit/term"
)

// Options configures a [LineHandler].
type Options struct {
	// Level is the lowest level that is written; nil means slog.LevelInfo,
	// as for the handlers of log/slog. A --verbose flag usually sets
	// slog.LevelDebug.
	Level slog.Leveler
}

// LineHandler is a slog.Handler that writes each record as one plain line:
// the message, then the attributes as key=value, separated by spaces, with
// neither time nor level:
//
//	sent a request method=GET path=/v2/projects status=200
//
// The attributes of [LineHandler.WithAttrs] come first, then those of the
// record. An attribute in a group, from [LineHandler.WithGroup] or a group
// value, has the group's name in front of its key, separated by a dot, as
// slog.TextHandler writes it. Keys and values are written as
// slog.Value.String gives them, made plain text with term.Plain, and in
// double quotes, as strconv.Quote writes them, when they hold anything but
// letters, digits and -._/:@+%~ — white space, a newline or an equals sign,
// for example. A value therefore stays on its line, and where it ends is
// never in doubt.
//
// The message goes through term.Plain as well. Then the message and each
// key=value pair go through secret.RedactPatterns, each on its own, so
// that text in one cannot hide a credential in another: escape sequences
// and control characters but the newline are taken out, and recognizable
// credentials such as password=… or Bearer tokens are replaced, a quoted
// value as a whole. A message with a newline still spans several lines. The handler does not
// know the secrets the program holds; pass those through secret.Redact
// before they reach a log record.
//
// A LineHandler and the handlers derived from it share one lock, so lines
// written from several goroutines do not interleave.
type LineHandler struct {
	w      io.Writer
	mu     *sync.Mutex
	level  slog.Leveler
	attrs  string   // the formatted attributes of WithAttrs
	groups []string // the groups of WithGroup, outermost first
}

// NewLineHandler returns a LineHandler that writes to w. opts may be nil.
func NewLineHandler(w io.Writer, opts *Options) *LineHandler {
	h := &LineHandler{w: w, mu: &sync.Mutex{}, level: slog.LevelInfo}
	if opts != nil && opts.Level != nil {
		h.level = opts.Level
	}
	return h
}

// Enabled reports whether a record at level is written.
func (h *LineHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

// Handle writes the record as one line.
func (h *LineHandler) Handle(_ context.Context, record slog.Record) error {
	var line strings.Builder
	line.WriteString(secret.RedactPatterns(term.Plain(record.Message)))
	line.WriteString(h.attrs)
	record.Attrs(func(attr slog.Attr) bool {
		appendAttr(&line, h.groups, attr)
		return true
	})
	text := line.String() + "\n"
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, text)
	return err
}

// WithAttrs returns a handler that writes attrs, in the groups h has, in
// front of the attributes of every record.
func (h *LineHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	var b strings.Builder
	b.WriteString(h.attrs)
	for _, attr := range attrs {
		appendAttr(&b, h.groups, attr)
	}
	derived := *h
	derived.attrs = b.String()
	return &derived
}

// WithGroup returns a handler that puts the attributes added after it into
// the group name. An empty name returns h, as slog asks.
func (h *LineHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	derived := *h
	derived.groups = append(slices.Clip(h.groups), name)
	return &derived
}

// appendAttr writes attr as " key=value" to b, with the names of groups in
// front of its key and credentials redacted. A group value writes each of its attributes so; an
// attribute without a key is left out, and so are the attributes of a group
// without a key inlined, as slog asks of a handler.
func appendAttr(b *strings.Builder, groups []string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Value.Kind() == slog.KindGroup {
		inner := groups
		if attr.Key != "" {
			inner = append(slices.Clip(groups), attr.Key)
		}
		for _, member := range attr.Value.Group() {
			appendAttr(b, inner, member)
		}
		return
	}
	if attr.Key == "" {
		return
	}
	var key strings.Builder
	for _, group := range groups {
		key.WriteString(group)
		key.WriteByte('.')
	}
	key.WriteString(attr.Key)
	b.WriteByte(' ')
	b.WriteString(secret.RedactPatterns(quote(key.String()) + "=" + quote(attr.Value.String())))
}

// quote returns s as plain text, in double quotes when it is empty or holds
// anything but letters, digits and -._/:@+%~.
func quote(s string) string {
	s = term.Plain(s)
	if s != "" && !strings.ContainsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("-._/:@+%~", r)
	}) {
		return s
	}
	return strconv.Quote(s)
}
