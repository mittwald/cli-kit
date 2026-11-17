package browser

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

// stub replaces the launcher with one that records what it was asked to
// run, for the platform goos, until the test ends.
func stub(t *testing.T, platform string, fail error) *[]string {
	t.Helper()
	oldStart, oldGOOS := start, goos
	t.Cleanup(func() { start, goos = oldStart, oldGOOS })
	var ran []string
	goos = platform
	start = func(_ context.Context, name string, args []string) error {
		ran = append([]string{name}, args...)
		return fail
	}
	return &ran
}

func TestOpenLaunchers(t *testing.T) {
	const page = "https://example.com/login?state=a%20b&x=1"
	tests := []struct {
		goos string
		want []string
	}{
		{"darwin", []string{"open", page}},
		{"windows", []string{"rundll32", "url.dll,FileProtocolHandler", page}},
		{"linux", []string{"xdg-open", page}},
		{"freebsd", []string{"xdg-open", page}},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			ran := stub(t, tt.goos, nil)
			if err := Open(context.Background(), page); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(*ran, tt.want) {
				t.Errorf("ran %q, want %q", *ran, tt.want)
			}
		})
	}
}

func TestOpenRefuses(t *testing.T) {
	for _, bad := range []string{
		"",
		"example.com",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"ftp://example.com/",
		"-https://example.com",
		"https:",
		"https:///path",
		"https:opaque",
		"https://example.com/\x1b]52;c;ZXZpbA==\x07",
		"https://example.com/\nsecond",
		"https://example.com/a b",
		"https://example.com/\u0085",
		"https://example.com/\xff",
		"https://[::1",
	} {
		t.Run(bad, func(t *testing.T) {
			ran := stub(t, "linux", nil)
			err := Open(context.Background(), bad)
			if !errors.Is(err, ErrInvalidURL) {
				t.Errorf("Open(%q) = %v, want ErrInvalidURL", bad, err)
			}
			if bad != "" && err != nil && strings.Contains(err.Error(), bad) {
				t.Errorf("the error repeats the URL: %v", err)
			}
			if len(*ran) != 0 {
				t.Errorf("Open(%q) ran %q", bad, *ran)
			}
		})
	}
	for _, good := range []string{"http://localhost:8080/callback", "HTTPS://Example.com", "https://user@example.com/"} {
		stub(t, "linux", nil)
		if err := Open(context.Background(), good); err != nil {
			t.Errorf("Open(%q) = %v", good, err)
		}
	}
}

func TestOpenErrors(t *testing.T) {
	ran := stub(t, "linux", errors.New("not found"))
	if err := Open(context.Background(), "https://example.com"); err == nil || !strings.Contains(err.Error(), "xdg-open") {
		t.Errorf("a launcher that does not start: %v", err)
	}

	*ran = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Open(ctx, "https://example.com"); !errors.Is(err, context.Canceled) || len(*ran) != 0 {
		t.Errorf("a canceled context: %v, ran %q", err, *ran)
	}
}

// The real launcher starts a command without waiting for it, and reports
// one that cannot be found.
func TestStartCommand(t *testing.T) {
	// The test binary itself, told to run no test, stands in for a launcher.
	if err := startCommand(context.Background(), os.Args[0], []string{"-test.run=^$"}); err != nil {
		t.Errorf("start: %v", err)
	}
	if err := startCommand(context.Background(), "cli-kit-no-such-launcher", nil); err == nil {
		t.Error("a missing launcher started")
	}
}
