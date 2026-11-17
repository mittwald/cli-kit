package logx

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/slogtest"
	"time"
)

func TestLineHandler(t *testing.T) {
	tests := []struct {
		name string
		log  func(*slog.Logger)
		want string
	}{
		{"message alone", func(l *slog.Logger) { l.Info("hello") }, "hello\n"},
		{"attributes", func(l *slog.Logger) { l.Info("sent", "method", "GET", "status", 200) }, "sent method=GET status=200\n"},
		{
			"handler attributes come first",
			func(l *slog.Logger) { l.With("run", 1).With("step", "b").Info("x", "k", "v") },
			"x run=1 step=b k=v\n",
		},
		{
			"groups qualify later attributes",
			func(l *slog.Logger) { l.With("a", 1).WithGroup("req").With("id", 7).Info("x", "path", "/") },
			"x a=1 req.id=7 req.path=/\n",
		},
		{"nested groups", func(l *slog.Logger) { l.WithGroup("a").WithGroup("b").Info("x", "k", "v") }, "x a.b.k=v\n"},
		{"empty group name", func(l *slog.Logger) { l.WithGroup("").Info("x", "k", "v") }, "x k=v\n"},
		{"group value", func(l *slog.Logger) { l.Info("x", slog.Group("g", "k", "v", slog.Group("h", "m", 1))) }, "x g.k=v g.h.m=1\n"},
		{"inline group", func(l *slog.Logger) { l.Info("x", slog.Group("", "k", "v")) }, "x k=v\n"},
		{"empty group", func(l *slog.Logger) { l.Info("x", slog.Group("g")) }, "x\n"},
		{"empty key", func(l *slog.Logger) { l.Info("x", slog.String("", "v")) }, "x\n"},
		{"log valuer", func(l *slog.Logger) { l.Info("x", "user", user("ann")) }, "x user=ann\n"},
		{"debug left out", func(l *slog.Logger) { l.Debug("x") }, ""},
		{"error", func(l *slog.Logger) { l.Error("failed", "err", errors.New("boom")) }, "failed err=boom\n"},
		{
			"escape sequences go",
			func(l *slog.Logger) { l.Info("\x1b]0;title\x07msg \x1b[31mred\x1b[0m", "k", "a\x1b[2Jb") },
			"msg red k=ab\n",
		},
		{
			"a value cannot end the line",
			func(l *slog.Logger) { l.Info("x", "k", "v\nforged line") },
			"x k=\"v\\nforged line\"\n",
		},
		{
			"credentials are redacted",
			func(l *slog.Logger) {
				l.With("password", "hunter2").Info("auth", "header", "Authorization: Bearer abc.def.ghi")
			},
			"auth password=[REDACTED] header=\"Authorization: [REDACTED]\"\n",
		},
		{
			"a credential with white space goes as a whole",
			func(l *slog.Logger) { l.Info("auth", "password", "two words", "token", "a,b;c") },
			"auth password=\"[REDACTED]\" token=\"[REDACTED]\"\n",
		},
		{
			"a message cannot hide a credential",
			func(l *slog.Logger) { l.Info("received \"bad", "password", ",hunter2") },
			"received \"bad password=\"[REDACTED]\"\n",
		},
		{
			"a credential in the message",
			func(l *slog.Logger) { l.Info("retry with token=abc123", "n", 1) },
			"retry with token=[REDACTED] n=1\n",
		},
		{
			"values that need it are quoted",
			func(l *slog.Logger) {
				l.Info("x", "path", "/v2/p?limit=1", "empty", "", "lines", "a\nb", "quote", `say "hi"`, "my key", "ü-1.5s")
			},
			"x path=\"/v2/p?limit=1\" empty=\"\" lines=\"a\\nb\" quote=\"say \\\"hi\\\"\" \"my key\"=ü-1.5s\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tt.log(slog.New(NewLineHandler(&buf, nil)))
			if got := buf.String(); got != tt.want {
				t.Errorf("logged %q, want %q", got, tt.want)
			}
		})
	}
}

// user is a slog.LogValuer.
type user string

func (u user) LogValue() slog.Value { return slog.StringValue(string(u)) }

func TestLineHandlerLevel(t *testing.T) {
	var buf bytes.Buffer
	level := new(slog.LevelVar)
	level.Set(slog.LevelWarn)
	l := slog.New(NewLineHandler(&buf, &Options{Level: level}))
	l.Info("hidden")
	l.Warn("shown")
	level.Set(slog.LevelDebug)
	l.Debug("debug")
	if got := buf.String(); got != "shown\ndebug\n" {
		t.Errorf("logged %q", got)
	}
}

// Lines written from several goroutines through derived handlers stay
// whole.
func TestLineHandlerConcurrent(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(NewLineHandler(&buf, nil))
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			l := base.With("worker", i)
			for range 50 {
				l.Info("tick", "k", "v")
			}
		})
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 400 {
		t.Fatalf("%d lines, want 400", len(lines))
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "tick worker=") || !strings.HasSuffix(line, " k=v") {
			t.Fatalf("broken line %q", line)
		}
	}
}

// The handler keeps the rules of log/slog for attributes and groups, which
// slogtest checks against the attributes it parses back from each line.
func TestLineHandlerSlogtest(t *testing.T) {
	var buf bytes.Buffer
	h := NewLineHandler(&buf, &Options{Level: slog.LevelDebug})
	results := func() []map[string]any {
		var records []map[string]any
		for line := range strings.Lines(buf.String()) {
			records = append(records, parseLine(strings.TrimSuffix(line, "\n")))
		}
		return records
	}
	if err := slogtest.TestHandler(h, results); err != nil {
		// The handler writes neither time nor level, which slogtest
		// expects; every other rule must hold.
		for _, e := range unwrapAll(err) {
			msg := e.Error()
			if !strings.HasPrefix(msg, `missing key "`+slog.TimeKey+`"`) && !strings.HasPrefix(msg, `missing key "`+slog.LevelKey+`"`) {
				t.Error(msg)
			}
		}
	}
}

// parseLine reads a line back into the nested map slogtest compares: the
// message under its key and each dotted key as nested groups.
func parseLine(line string) map[string]any {
	fields := strings.Fields(line)
	record := map[string]any{}
	var message []string
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			message = append(message, field)
			continue
		}
		parts := strings.Split(key, ".")
		m := record
		for _, group := range parts[:len(parts)-1] {
			inner, ok := m[group].(map[string]any)
			if !ok {
				inner = map[string]any{}
				m[group] = inner
			}
			m = inner
		}
		m[parts[len(parts)-1]] = value
	}
	record[slog.MessageKey] = strings.Join(message, " ")
	return record
}

// unwrapAll returns the errors joined in err.
func unwrapAll(err error) []error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return joined.Unwrap()
	}
	return []error{err}
}

func TestLineHandlerWriteError(t *testing.T) {
	h := NewLineHandler(failingWriter{}, nil)
	if err := h.Handle(context.Background(), slog.NewRecord(timeZero, slog.LevelInfo, "x", 0)); err == nil {
		t.Error("a failed write was not reported")
	}
}

// timeZero is the time of a record that has none.
var timeZero time.Time

// failingWriter fails every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
