package browser

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrInvalidURL reports a URL that [Open] refuses to hand to a launcher.
var ErrInvalidURL = errors.New("browser: invalid URL")

// Seams for tests: the real implementation starts a command.
var (
	start = startCommand
	goos  = runtime.GOOS
)

// Open opens rawURL in the user's browser and returns once the launcher
// has started; it does not wait for the page to open, and a launcher that
// fails later goes unnoticed. ctx bounds the launcher: when ctx ends while
// it still runs, it is killed.
//
// Only an absolute http or https URL with a host is opened. A URL that is
// not valid UTF-8 or holds white space or a control character (C0, DEL or
// C1) is refused as well, since it is handed to a program that may read it
// further: escape such characters as %XX. A refused URL is reported with an
// error wrapping [ErrInvalidURL] that does not repeat the URL, which may
// carry a credential.
//
// The launcher is open on macOS, rundll32 url.dll,FileProtocolHandler on
// Windows, and xdg-open on every other platform. It is found through PATH
// and gets the URL as one argument, never through a shell. Its output is
// discarded.
func Open(ctx context.Context, rawURL string) error {
	if err := check(rawURL); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("browser: %w", err)
	}
	name, args := launcher(goos, rawURL)
	if err := start(ctx, name, args); err != nil {
		return fmt.Errorf("browser: start %s: %w", name, err)
	}
	return nil
}

// check refuses a URL that Open does not hand to a launcher.
func check(rawURL string) error {
	if !utf8.ValidString(rawURL) || strings.ContainsFunc(rawURL, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}) {
		return fmt.Errorf("%w: it holds white space, a control character or invalid UTF-8", ErrInvalidURL)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: it cannot be parsed", ErrInvalidURL)
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
		return fmt.Errorf("%w: only http and https URLs are opened", ErrInvalidURL)
	}
	if u.Host == "" || u.Opaque != "" {
		return fmt.Errorf("%w: the URL names no host", ErrInvalidURL)
	}
	return nil
}

// launcher returns the command that opens rawURL on the platform goos.
func launcher(goos, rawURL string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{rawURL}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", rawURL}
	}
	return "xdg-open", []string{rawURL}
}

// startCommand starts name with args and reaps it in the background; its
// outcome says nothing about whether a page opened.
func startCommand(ctx context.Context, name string, args []string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // name is one of the fixed launchers; the URL is checked and passed as one argument, never to a shell
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
