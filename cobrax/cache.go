package cobrax

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"
)

// Defaults for [CompletionCache].
const (
	// DefaultCompletionTTL is how long cached candidates are offered.
	DefaultCompletionTTL = time.Hour
	// DefaultCompletionTimeout bounds a load on a cold cache: a tab press that
	// hangs is worse than one that offers nothing.
	DefaultCompletionTimeout = 10 * time.Second
)

// CompletionCache keeps completion candidates that are slow to load, such as
// names fetched from a remote service, in a file under the user's cache
// directory, so a tab press answers from disk. Candidates are a hint for the
// shell, never what a command acts on, so a stale list costs little.
type CompletionCache[T any] struct {
	// App names the directory under the cache directory, usually the
	// command's name. It is required.
	App string
	// TTL is how long a cached value is used; zero means
	// [DefaultCompletionTTL].
	TTL time.Duration
	// Timeout bounds one load; zero means [DefaultCompletionTimeout].
	Timeout time.Duration
	// Dir overrides the base directory, os.UserCacheDir by default. Where
	// neither is available, values are loaded uncached.
	Dir string
	// Keep decides whether a loaded value is worth caching. An empty answer
	// often means "not signed in yet" rather than "nothing exists", and
	// caching it would keep completion silent until the TTL runs out. Nil
	// caches every value.
	Keep func(T) bool
}

// errNoApp is returned when CompletionCache.App is empty.
var errNoApp = errors.New("cobrax: CompletionCache.App is empty")

// Get returns the value cached for scope while it is younger than the TTL,
// and otherwise calls load, bounded by the timeout, and caches its result.
// scope identifies what the value answers, such as a repository URL; it is
// hashed into the file name. Failing to write the cache is not an error, and
// where there is no cache directory, such as without $HOME, Get calls load
// every time.
func (c CompletionCache[T]) Get(ctx context.Context, scope string, load func(context.Context) (T, error)) (T, error) {
	var zero T
	if c.App == "" {
		return zero, errNoApp
	}
	path, cached := c.path(scope)
	if cached {
		if value, ok := c.read(path); ok {
			return value, nil
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultCompletionTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	value, err := load(ctx)
	if err != nil {
		return zero, err
	}
	if cached && (c.Keep == nil || c.Keep(value)) {
		write(path, value)
	}
	return value, nil
}

// path returns the file caching scope, and false where there is no cache
// directory.
func (c CompletionCache[T]) path(scope string) (string, bool) {
	dir := c.Dir
	if dir == "" {
		var err error
		if dir, err = os.UserCacheDir(); err != nil {
			return "", false
		}
	}
	sum := sha256.Sum256([]byte(scope))
	return filepath.Join(dir, c.App, "completion", hex.EncodeToString(sum[:8])+".json"), true
}

func (c CompletionCache[T]) read(path string) (T, bool) {
	var value T
	ttl := c.TTL
	if ttl <= 0 {
		ttl = DefaultCompletionTTL
	}
	info, err := os.Stat(path)
	if err != nil {
		return value, false
	}
	// A file dated in the future, after a clock change or a restored
	// backup, would otherwise stay fresh until the clock caught up with it.
	if age := time.Since(info.ModTime()); age < 0 || age >= ttl {
		return value, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return value, false
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return value, false
	}
	return value, true
}

// write stores value at path, best effort. It writes a temporary file next
// to the target and renames it, so a completion running alongside never
// reads half a file.
func write(path string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "cache-*.json")
	if err != nil {
		return
	}
	defer func() { _ = os.Remove(temporary.Name()) }()
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return
	}
	if err := temporary.Close(); err != nil {
		return
	}
	_ = os.Rename(temporary.Name(), path)
}

// CachedCompletion returns a completion function that offers the strings
// load returns, cached per scope for ttl under the cache directory of app.
// load runs at most timeout long; zero ttl or timeout use the defaults. An
// empty result is not cached. A failing load completes nothing; the error is
// written to cobra's completion debug log.
//
// What load returns usually comes from elsewhere, such as names a remote
// service lists, and the shell shows it as it is. A candidate that holds a
// control character, an escape sequence among them, is therefore left out:
// it could not be completed to the name it stands for anyway. The one tab
// that separates a value from its description, as cobra.CompletionWithDesc
// writes it, is kept.
func CachedCompletion(app, scope string, ttl, timeout time.Duration, load func(context.Context) ([]string, error)) cobra.CompletionFunc {
	cache := CompletionCache[[]string]{
		App:     app,
		TTL:     ttl,
		Timeout: timeout,
		Keep:    func(values []string) bool { return len(values) > 0 },
	}
	return func(cmd *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) {
		values, err := cache.Get(cmd.Context(), scope, load)
		if err != nil {
			cobra.CompDebugln("cached completion: "+err.Error(), false)
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return slices.DeleteFunc(slices.Clone(values), unsafeCompletion), cobra.ShellCompDirectiveNoFileComp
	}
}

// unsafeCompletion reports whether a candidate holds a control character
// other than the tab between its value and its description.
func unsafeCompletion(candidate string) bool {
	value, description, _ := strings.Cut(candidate, "\t")
	return value == "" || strings.ContainsFunc(value, unicode.IsControl) ||
		strings.ContainsFunc(description, unicode.IsControl)
}
