package cobrax

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/mittwald/cli-kit/buildinfo"
	"github.com/mittwald/cli-kit/term"
)

// keepColorMode restores the process-wide color mode after a test.
func keepColorMode(t *testing.T) {
	t.Helper()
	previous := term.CurrentColorMode()
	t.Cleanup(func() { term.SetColorMode(previous) })
}

// newRoot returns a root command with two subcommands, so cobra adds its
// completion command.
func newRoot() *cobra.Command {
	root := &cobra.Command{Use: "example", SilenceErrors: true, SilenceUsage: true}
	list := &cobra.Command{Use: "list", RunE: func(*cobra.Command, []string) error { return nil }}
	list.Flags().String("file", "", "a file")
	list.Flags().String("name", "", "a name")
	list.Flags().Bool("all", false, "everything")
	show := &cobra.Command{Use: "show", RunE: func(*cobra.Command, []string) error { return nil }}
	root.AddCommand(list, show)
	return root
}

// execute runs root with args and returns stdout and the error.
func execute(t *testing.T, root *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), err
}

// complete asks root to complete args and returns the candidates and the
// directive line.
func complete(t *testing.T, root *cobra.Command, args ...string) ([]string, string) {
	t.Helper()
	out, err := execute(t, root, append([]string{cobra.ShellCompNoDescRequestCmd}, args...)...)
	if err != nil {
		t.Fatalf("complete %q: %v", args, err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var candidates []string
	directive := ""
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, ":"):
			directive = line
		case strings.HasPrefix(line, "Completion ended"):
		default:
			candidates = append(candidates, line)
		}
	}
	return candidates, directive
}

const noFileDirective = ":4"

func TestColorFlag(t *testing.T) {
	keepColorMode(t)
	for _, tc := range []struct {
		args []string
		want term.ColorMode
	}{
		{[]string{"show"}, term.ColorAuto},
		{[]string{"--color", "show"}, term.ColorAlways},
		{[]string{"--color=never", "show"}, term.ColorNever},
		{[]string{"--colour=always", "show"}, term.ColorAlways}, //nolint:misspell // British alias.
		{[]string{"show", "--color=auto"}, term.ColorAuto},
	} {
		term.SetColorMode(term.ColorAuto)
		root := newRoot()
		value := AddColorFlag(root, "when to use color", "colour") //nolint:misspell // British alias.
		if _, err := execute(t, root, tc.args...); err != nil {
			t.Fatalf("%q: %v", tc.args, err)
		}
		if value.Mode() != tc.want || term.CurrentColorMode() != tc.want {
			t.Errorf("%q: mode = %v, term = %v, want %v", tc.args, value.Mode(), term.CurrentColorMode(), tc.want)
		}
	}

	root := newRoot()
	AddColorFlag(root, "when to use color")
	if _, err := execute(t, root, "--color=rainbow", "show"); err == nil {
		t.Error("an invalid color mode was accepted")
	}

	root = newRoot()
	AddColorFlag(root, "when to use color")
	// A bare --color is complete on its own, so values complete after "=".
	candidates, directive := complete(t, root, "--color=")
	want := term.ColorModeNames
	if !slices.Equal(candidates, want) || directive != noFileDirective {
		t.Errorf("--color completion = %q %s", candidates, directive)
	}
}

// The options change what a bare flag means, which values are accepted and
// how a rejected one is worded, and nothing else.
func TestColorFlagWith(t *testing.T) {
	keepColorMode(t)
	grep := []ColorOption{ColorAliases("colour"), BareColor(term.ColorAuto), StrictColor(), //nolint:misspell // British alias.
		ColorError(func(value string) error { return fmt.Errorf("Invalid --color %q (want auto, always, or never)", value) })}
	for _, tc := range []struct {
		opts []ColorOption
		args []string
		want term.ColorMode
		err  string
	}{
		{nil, []string{"--color", "show"}, term.ColorAlways, ""},
		{nil, []string{"--color=yes", "show"}, term.ColorAlways, ""},
		{nil, []string{"--color=tty", "show"}, term.ColorAuto, ""},
		{grep, []string{"show"}, term.ColorAuto, ""},
		{grep, []string{"--color", "show"}, term.ColorAuto, ""},
		{grep, []string{"--colour", "show"}, term.ColorAuto, ""}, //nolint:misspell // British alias.
		{grep, []string{"--color=", "show"}, term.ColorAuto, ""},
		{grep, []string{"--color=TRUE", "show"}, term.ColorAuto, ""},
		{grep, []string{"--color=Always", "show"}, term.ColorAlways, ""},
		{grep, []string{"--color=never", "show"}, term.ColorNever, ""},
		{grep, []string{"--color=yes", "show"}, term.ColorAuto,
			`invalid argument "yes" for "--color" flag: Invalid --color "yes" (want auto, always, or never)`},
		{grep, []string{"--color=false", "show"}, term.ColorAuto,
			`invalid argument "false" for "--color" flag: Invalid --color "false" (want auto, always, or never)`},
		{[]ColorOption{StrictColor()}, []string{"--color=none", "show"}, term.ColorAuto,
			`invalid argument "none" for "--color" flag: invalid color mode "none": want one of auto, always, never`},
		{[]ColorOption{BareColor(term.ColorNever)}, []string{"--color=true", "show"}, term.ColorNever, ""},
	} {
		term.SetColorMode(term.ColorAuto)
		root := newRoot()
		value := AddColorFlagWith(root, "when to use color", tc.opts...)
		_, err := execute(t, root, tc.args...)
		switch {
		case tc.err == "" && err != nil:
			t.Errorf("%q: %v", tc.args, err)
		case tc.err != "" && (err == nil || err.Error() != tc.err):
			t.Errorf("%q: error = %v, want %s", tc.args, err, tc.err)
		case value.Mode() != tc.want || term.CurrentColorMode() != tc.want:
			t.Errorf("%q: mode = %v, term = %v, want %v", tc.args, value.Mode(), term.CurrentColorMode(), tc.want)
		}
	}

	// The flag's bare value follows the option, for help and completion.
	root := newRoot()
	AddColorFlagWith(root, "when to use color", grep...)
	for _, name := range []string{"color", "colour"} { //nolint:misspell // British alias.
		if got := root.PersistentFlags().Lookup(name).NoOptDefVal; got != "auto" {
			t.Errorf("--%s bare value = %q, want auto", name, got)
		}
	}
}

func TestEnum(t *testing.T) {
	enum := NewEnum("text", "text", "json", "yaml").WithType("format")
	if err := enum.Set("JSON"); err != nil || enum.Value() != "json" {
		t.Fatalf("Set(JSON) = %v, value %q", err, enum.Value())
	}
	err := enum.Set("xml")
	if err == nil || !strings.Contains(err.Error(), "text, json, yaml") {
		t.Fatalf("Set(xml) error = %v", err)
	}
	if enum.Type() != "format" || enum.String() != "json" || !slices.Equal(enum.Allowed(), []string{"text", "json", "yaml"}) {
		t.Errorf("enum = %q %q %q", enum.Type(), enum.String(), enum.Allowed())
	}

	root := newRoot()
	output := AddEnumFlag(root, root.PersistentFlags(), "output", "o", "text", "output format", "text", "json")
	if _, err := execute(t, root, "show", "-o", "json"); err != nil || output.Value() != "json" {
		t.Fatalf("-o json: err %v, value %q", err, output.Value())
	}
	root = newRoot()
	AddEnumFlag(root, root.PersistentFlags(), "output", "o", "text", "output format", "text", "json")
	candidates, directive := complete(t, root, "show", "--output", "")
	if !slices.Equal(candidates, []string{"text", "json"}) || directive != noFileDirective {
		t.Errorf("--output completion = %q %s", candidates, directive)
	}
}

func TestNoFileCompletions(t *testing.T) {
	root := newRoot()
	list, _, _ := root.Find([]string{"list"})
	mustRegister(list, "name", CompleteValues("alice", "bob"))
	NoFileCompletions(root)

	if _, directive := complete(t, root, "list", "--file", ""); directive != noFileDirective {
		t.Errorf("--file directive = %s, want %s", directive, noFileDirective)
	}
	if candidates, _ := complete(t, root, "list", "--name", ""); !slices.Equal(candidates, []string{"alice", "bob"}) {
		t.Errorf("an existing completion was replaced: %q", candidates)
	}

	root = newRoot()
	list, _, _ = root.Find([]string{"list"})
	NoFileCompletions(list, "file")
	if _, directive := complete(t, root, "list", "--file", ""); directive != noFileDirective {
		t.Errorf("named --file directive = %s", directive)
	}
	if _, directive := complete(t, root, "list", "--name", ""); directive == noFileDirective {
		t.Error("an unnamed flag lost file completion")
	}

	defer func() {
		if recover() == nil {
			t.Error("NoFileCompletions accepted a missing flag")
		}
	}()
	NoFileCompletions(list, "missing")
}

func TestNoFileArgs(t *testing.T) {
	root := newRoot()
	show, _, _ := root.Find([]string{"show"})
	show.ValidArgs = []string{"one", "two"}
	NoFileArgs(root)
	list, _, _ := root.Find([]string{"list"})
	if list.ValidArgsFunction == nil {
		t.Error("list kept file completion for its arguments")
	}
	if show.ValidArgsFunction != nil {
		t.Error("show's own argument completion was replaced")
	}
}

func TestHardenCompletionScripts(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			root := newRoot()
			if HardenCompletion(root) == nil {
				t.Fatal("no completion command")
			}
			first, err := execute(t, root, "completion", shell)
			if err != nil || !strings.HasPrefix(first, "#") || !strings.Contains(first, "__complete") {
				t.Fatalf("script: err %v, output %.80q", err, first)
			}
			second, _ := execute(t, root, "completion", shell)
			if first != second {
				t.Error("generation is not deterministic")
			}
			noDesc, err := execute(t, root, "completion", shell, "--no-descriptions")
			if err != nil || noDesc == "" {
				t.Errorf("--no-descriptions: err %v", err)
			}

			root.SetOut(failingWriter{})
			root.SetArgs([]string{"completion", shell})
			var scriptErr *ScriptError
			if err := root.Execute(); !errors.As(err, &scriptErr) || scriptErr.Shell != shell {
				t.Errorf("write failure: err = %v, want *ScriptError", err)
			}
		})
	}
}

func TestHardenCompletionRejectsUnknownShell(t *testing.T) {
	root := newRoot()
	HardenCompletion(root)
	_, err := execute(t, root, "completion", "tcsh")
	if err == nil || !strings.Contains(err.Error(), `unknown command "tcsh"`) {
		t.Fatalf("completion tcsh: err = %v", err)
	}
	if _, ok := errors.AsType[*ScriptError](err); ok {
		t.Error("an unknown shell was reported as a script error")
	}
	if _, err := execute(t, root, "completion"); err != nil {
		t.Errorf("bare completion: %v", err)
	}
}

func TestHardenCompletionWithoutSubcommands(t *testing.T) {
	root := &cobra.Command{Use: "example", RunE: func(*cobra.Command, []string) error { return nil }}
	if HardenCompletion(root) != nil {
		t.Error("a root without subcommands got a completion command")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestCompletionShellLoad(t *testing.T) {
	for _, tc := range []struct {
		shell, executable string
		args              []string
	}{
		{"bash", "bash", []string{"--noprofile", "--norc", "-c", `source "$1"; complete -p example`, "_"}},
		{"zsh", "zsh", []string{"-f", "-c", `autoload -Uz compinit; compinit -D; source "$1"; [[ ${_comps[example]} == _example ]]`, "_"}},
		{"fish", "fish", []string{"--no-config", "-c", `source $argv[1]; complete | string match -q '*example*'`}},
		{"powershell", "pwsh", []string{"-NoProfile", "-NonInteractive", "-File"}},
	} {
		t.Run(tc.shell, func(t *testing.T) {
			executable, err := exec.LookPath(tc.executable)
			if err != nil {
				t.Skipf("%s not installed", tc.executable)
			}
			root := newRoot()
			HardenCompletion(root)
			script, err := execute(t, root, "completion", tc.shell)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "completion.ps1")
			if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command(executable, append(tc.args, path)...).CombinedOutput(); err != nil {
				t.Fatalf("load: %v\n%s", err, output)
			}
		})
	}
}

func TestCompletionCache(t *testing.T) {
	cache := CompletionCache[[]string]{App: "example", Dir: t.TempDir(), TTL: time.Hour}
	calls := 0
	load := func(ctx context.Context) ([]string, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Error("load ran without a deadline")
		}
		return []string{"one", "two"}, nil
	}
	for range 2 {
		got, err := cache.Get(context.Background(), "scope", load)
		if err != nil || !slices.Equal(got, []string{"one", "two"}) {
			t.Fatalf("Get = %q, %v", got, err)
		}
	}
	if calls != 1 {
		t.Fatalf("load ran %d times, want 1", calls)
	}

	path, _ := cache.path("scope")
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Get(nil, "scope", load); err != nil || calls != 2 { //nolint:staticcheck // A nil context is allowed.
		t.Fatalf("an expired cache was used: calls %d, err %v", calls, err)
	}
	if _, err := cache.Get(context.Background(), "other", load); err != nil || calls != 3 {
		t.Fatalf("scopes share a cache entry: calls %d", calls)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no Unix permission bits; Go reports 0777 for directories.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Errorf("cache directory mode = %v", info.Mode().Perm())
	}
}

func TestCompletionCacheKeepAndErrors(t *testing.T) {
	cache := CompletionCache[[]string]{
		App:  "example",
		Dir:  t.TempDir(),
		Keep: func(values []string) bool { return len(values) > 0 },
	}
	calls := 0
	empty := func(context.Context) ([]string, error) { calls++; return nil, nil }
	for range 2 {
		if _, err := cache.Get(context.Background(), "scope", empty); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Errorf("an empty result was cached: calls %d", calls)
	}
	failure := errors.New("offline")
	if _, err := cache.Get(context.Background(), "scope", func(context.Context) ([]string, error) { return nil, failure }); !errors.Is(err, failure) {
		t.Errorf("load error = %v", err)
	}
	if _, err := (CompletionCache[int]{}).Get(context.Background(), "x", nil); err == nil {
		t.Error("a cache without App was accepted")
	}
}

// Without a cache directory, such as without $HOME, every call loads.
func TestCompletionCacheWithoutCacheDirectory(t *testing.T) {
	for _, name := range []string{"HOME", "XDG_CACHE_HOME", "LocalAppData"} {
		t.Setenv(name, "")
	}
	if dir, err := os.UserCacheDir(); err == nil {
		t.Skipf("os.UserCacheDir still answers %q", dir)
	}
	cache := CompletionCache[[]string]{App: "example"}
	calls := 0
	load := func(context.Context) ([]string, error) { calls++; return []string{"a"}, nil }
	for range 2 {
		values, err := cache.Get(context.Background(), "scope", load)
		if err != nil || len(values) != 1 || values[0] != "a" {
			t.Fatalf("Get = %v, %v; want the loaded values", values, err)
		}
	}
	if calls != 2 {
		t.Errorf("load ran %d times, want every call without a cache", calls)
	}
}

func TestCachedCompletion(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	root := newRoot()
	list, _, _ := root.Find([]string{"list"})
	mustRegister(list, "name", CachedCompletion("example", "names", 0, 0, func(context.Context) ([]string, error) {
		return []string{"alice", "bob"}, nil
	}))
	if candidates, directive := complete(t, root, "list", "--name", ""); !slices.Equal(candidates, []string{"alice", "bob"}) || directive != noFileDirective {
		t.Errorf("cached completion = %q %s", candidates, directive)
	}

	root = newRoot()
	list, _, _ = root.Find([]string{"list"})
	mustRegister(list, "name", CachedCompletion("example", "failing", 0, 0, func(context.Context) ([]string, error) {
		return nil, errors.New("offline")
	}))
	if candidates, directive := complete(t, root, "list", "--name", ""); len(candidates) != 0 || directive != noFileDirective {
		t.Errorf("failing completion = %q %s", candidates, directive)
	}
}

func spacedRoot() *cobra.Command {
	root := newRoot()
	deploy := &cobra.Command{Use: "deploy", RunE: func(*cobra.Command, []string) error { return nil }}
	for _, name := range []string{"active", "stable", "hidden", "apply"} {
		deploy.Flags().Bool(name, false, name)
	}
	deploy.Flags().String("previous", "", "previous version")
	root.PersistentFlags().BoolP("verbose", "v", false, "verbose")
	root.PersistentFlags().Bool("dry-run", false, "dry run")
	root.PersistentFlags().String("output", "", "output format")
	AllowSpacedBool(deploy.Flags(), "active", "stable", "hidden")
	AllowSpacedBool(root.PersistentFlags(), "dry-run")
	// status has a --stable flag of its own that is not marked.
	status := &cobra.Command{Use: "status", RunE: func(*cobra.Command, []string) error { return nil }}
	status.Flags().Bool("stable", false, "stable only")
	root.AddCommand(deploy, status)
	return root
}

func TestJoinSpacedBoolValues(t *testing.T) {
	root := spacedRoot()
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{"space form is joined",
			[]string{"deploy", "app-1.2", "--stable", "false", "--apply"},
			[]string{"deploy", "app-1.2", "--stable=false", "--apply"}},
		{"several at once",
			[]string{"deploy", "--active", "false", "--stable", "true"},
			[]string{"deploy", "--active=false", "--stable=true"}},
		{"bare flag stands alone", []string{"--stable", "--apply"}, []string{"--stable", "--apply"}},
		{"equals form untouched", []string{"--stable=false"}, []string{"--stable=false"}},
		{"flag at the end", []string{"deploy", "--stable"}, []string{"deploy", "--stable"}},
		{"unmarked flags keep their positional", []string{"--previous", "false", "--apply", "true"}, []string{"--previous", "false", "--apply", "true"}},
		{"non-boolean value not swallowed", []string{"--stable", "app-1.2"}, []string{"--stable", "app-1.2"}},
		{"case is normalized", []string{"deploy", "--active", "FALSE"}, []string{"deploy", "--active=false"}},
		{"nothing after -- is touched",
			[]string{"deploy", "--stable", "false", "--", "--active", "false"},
			[]string{"deploy", "--stable=false", "--", "--active", "false"}},
		{"flags before the command name",
			[]string{"--stable", "false", "deploy"},
			[]string{"--stable=false", "deploy"}},
		{"flags of the selected command only",
			[]string{"status", "--stable", "false"},
			[]string{"status", "--stable", "false"}},
		{"marked flags are not joined on the root",
			[]string{"--active", "false"},
			[]string{"--active", "false"}},
		{"marked persistent flag before a command",
			[]string{"--dry-run", "false", "deploy", "--stable", "true"},
			[]string{"--dry-run=false", "deploy", "--stable=true"}},
		{"marked persistent flag in a command",
			[]string{"status", "--dry-run", "true"},
			[]string{"status", "--dry-run=true"}},
		{"flag values do not select a command",
			[]string{"--output", "deploy", "status", "--stable", "false"},
			[]string{"--output", "deploy", "status", "--stable", "false"}},
		{"short flags are not candidates", []string{"-v", "false"}, []string{"-v", "false"}},
		{"completion keeps the word being completed",
			[]string{cobra.ShellCompRequestCmd, "deploy", "--active", "false", "--stable", "fal"},
			[]string{cobra.ShellCompRequestCmd, "deploy", "--active=false", "--stable", "fal"}},
		{"bare completion request", []string{cobra.ShellCompRequestCmd}, []string{cobra.ShellCompRequestCmd}},
		{"bare completion request without descriptions", []string{cobra.ShellCompNoDescRequestCmd}, []string{cobra.ShellCompNoDescRequestCmd}},
		{"completion of the first word", []string{cobra.ShellCompRequestCmd, "dep"}, []string{cobra.ShellCompRequestCmd, "dep"}},
		{"completion of a literal word",
			[]string{cobra.ShellCompRequestCmd, "deploy", "--stable", "false"},
			[]string{cobra.ShellCompRequestCmd, "deploy", "--stable", "false"}},
	} {
		if got := JoinSpacedBoolValues(root, tc.args); !slices.Equal(got, tc.want) {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
	if got := JoinSpacedBoolValues(newRoot(), []string{"--all", "false"}); !slices.Equal(got, []string{"--all", "false"}) {
		t.Errorf("unmarked tree rewrote arguments: %q", got)
	}
}

func TestJoinedArgumentsParse(t *testing.T) {
	root := spacedRoot()
	deploy, _, _ := root.Find([]string{"deploy"})
	if _, err := execute(t, root, JoinSpacedBoolValues(root, []string{"deploy", "--stable", "true"})...); err != nil {
		t.Fatal(err)
	}
	if stable, _ := deploy.Flags().GetBool("stable"); !stable {
		t.Error("--stable true did not set the flag")
	}
}

func TestAllowSpacedBoolRejectsNonBool(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a string flag was accepted")
		}
	}()
	root := spacedRoot()
	deploy, _, _ := root.Find([]string{"deploy"})
	AllowSpacedBool(deploy.Flags(), "previous")
}

func TestVersion(t *testing.T) {
	info := buildinfo.Info{Version: "v1.2.3", Commit: "0123456789abcdef", GoVersion: "go1.24.9"}
	root := newRoot()
	SetVersion(root, info)
	if out, err := execute(t, root, "--version"); err != nil || out != "example 1.2.3\n" {
		t.Errorf("--version = %q, %v", out, err)
	}
	root = newRoot()
	root.AddCommand(NewVersionCommand(info))
	out, err := execute(t, root, "version")
	if err != nil || out != "example 1.2.3\ncommit: 0123456789abcdef\ngo:     go1.24.9\n" {
		t.Errorf("version = %q, %v", out, err)
	}
}
