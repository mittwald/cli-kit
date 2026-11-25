# cli-kit

[![Go Reference](https://pkg.go.dev/badge/github.com/mittwald/cli-kit.svg)](https://pkg.go.dev/github.com/mittwald/cli-kit)
[![Test](https://github.com/mittwald/cli-kit/actions/workflows/test.yml/badge.svg)](https://github.com/mittwald/cli-kit/actions/workflows/test.yml)

cli-kit is the plumbing for Go command-line tools: deciding whether a stream
is a terminal and may be painted, interactive pickers and prompts, tables
and structured output, live status lines and progress bars, credentials,
self-updates, shell completion and version information.

Every package keeps output plain when it does not go to a terminal, so
pipes, files and tests see no escape sequences. Packages take their settings
as options; the only process-wide settings are the color mode and the
theme.

## Installation

```shell
go get github.com/mittwald/cli-kit
```

cli-kit supports Go 1.27 and newer on Linux, macOS and Windows. It is one
module: import only the packages you need, and only their dependencies are
compiled into your binary.

## Packages

| Package | What it does |
|---|---|
| [`term`](https://pkg.go.dev/github.com/mittwald/cli-kit/term) | Terminal detection and size, the `--color` mode with `NO_COLOR` and `TERM=dumb`, visible text width, truncation, padding, word wrapping, OSC 8 hyperlinks and when to write them, 24-bit color detection, and untrusted output made safe to show |
| [`theme`](https://pkg.go.dev/github.com/mittwald/cli-kit/theme) | SGR styles, styles parsed from user settings ("fg=white:bg=#aa0000:bold"), regular expression matches painted into painted text, the color roles the other packages paint with, unified diffs painted the way git paints them, painted JSON, and a stable color per name |
| [`pick`](https://pkg.go.dev/github.com/mittwald/cli-kit/pick) | Interactive list selection with filtering, views and ranges, per-row and in-cell painting, an adjustable look, switches, and line-edited text and secret input |
| [`live`](https://pkg.go.dev/github.com/mittwald/cli-kit/live) | Blocks of lines redrawn in place, detail/result output separation, and cursor and terminal restore on exit, panic and signals |
| [`progress`](https://pkg.go.dev/github.com/mittwald/cli-kit/progress) | A gradient progress bar with a stall countdown that steps aside for log lines |
| [`table`](https://pkg.go.dev/github.com/mittwald/cli-kit/table) | Tables in plain, framed and Markdown styles, and CSV/TSV records, with spreadsheet formulas escaped on request |
| [`format`](https://pkg.go.dev/github.com/mittwald/cli-kit/format) | An `--output` flag value, JSON/YAML writers driven by the same struct tags, and compact ages ("3h", "5mo") |
| [`ics`](https://pkg.go.dev/github.com/mittwald/cli-kit/ics) | An RFC 5545 iCalendar writer |
| [`secret`](https://pkg.go.dev/github.com/mittwald/cli-kit/secret) | Credentials from flags, files, environment, stdin, the OS keyring or a hidden prompt |
| [`redact`](https://pkg.go.dev/github.com/mittwald/cli-kit/redact) | Known secrets and recognizable credentials removed from text before it is logged, with the standard library only |
| [`selfupdate`](https://pkg.go.dev/github.com/mittwald/cli-kit/selfupdate) | Self-updates from GitHub Releases or a GitLab package registry, checked against the release's checksums file and, optionally, its minisign signature |
| [`cobrax`](https://pkg.go.dev/github.com/mittwald/cli-kit/cobrax) | Cobra helpers: `--color` and other auto/always/never flags, enum flags, deprecated flag spellings as hidden aliases, hardened, cached and prefix-filtered shell completion with an optional `--install` or a script written to a path, version command |
| [`fsx`](https://pkg.go.dev/github.com/mittwald/cli-kit/fsx) | Files replaced atomically with a chosen mode, refusing a symbolic link in their place |
| [`browser`](https://pkg.go.dev/github.com/mittwald/cli-kit/browser) | A web page opened in the user's browser, http and https URLs only, without a shell |
| [`notice`](https://pkg.go.dev/github.com/mittwald/cli-kit/notice) | Error, warning, info, OK and debug lines in line, badge, bar or block themes, with colors for dark and light terminals, colored values and patterns, made plain and redacted |
| [`logx`](https://pkg.go.dev/github.com/mittwald/cli-kit/logx) | A `log/slog` handler writing plain, redacted lines for `--verbose` |
| [`clipboard`](https://pkg.go.dev/github.com/mittwald/cli-kit/clipboard) | Text copied to the system clipboard through the platform's helper commands, without cgo |
| [`buildinfo`](https://pkg.go.dev/github.com/mittwald/cli-kit/buildinfo) | Version, commit and date from `-ldflags` or the Go build information |

## Examples

To see the interactive packages at work, run the demo program:

```shell
go run github.com/mittwald/cli-kit/examples/demo@latest --help
go run github.com/mittwald/cli-kit/examples/demo@latest pick
```

### Color and terminal

```go
cobrax.AddColorFlag(root, "when to use color: auto, always or never")
links := cobrax.AddModeFlag(root, "links", "when to make links: auto, always or never")
cobrax.AddFlagAlias(root, root.PersistentFlags(), // the old spelling, hidden, with a note
	cobrax.FlagAlias{Old: "no-links", New: "links", Value: "never"})

if term.Colored(os.Stdout) {
	fmt.Println(theme.Active().Success.Paint("done"))
}
width := term.Columns(os.Stdout, 80)
fmt.Println(term.Truncate(longLine, width))

th := theme.Active().For(os.Stdout) // theme.Plain where stdout takes no color
fmt.Print(th.PaintDiff(term.Plain(diff))) // a diff from elsewhere: made plain first
fmt.Println(term.Sanitize(jobLog))        // its colors kept, every other escape gone
fmt.Println(term.PlainLine(name))         // one line, no escape sequence, no color
fmt.Println(th.PaintJSON(string(data)))   // JSON from encoding/json: keys, strings, numbers, literals
author := th.Hashed(name)                 // the same color for the same name, on every run
if term.ShouldLink(links.Mode(), os.Stdout) {
	fmt.Println(term.Hyperlink(url, term.PlainLine(name)))
}
style, ok := theme.ParseStyle(os.Getenv("EXAMPLE_COLOR_ERROR")) // "red:bold", "245", "bg=#aa0000"
fmt.Println(style.For(os.Stdout).PaintMatches(line, errorPattern)) // the line keeps its own colors
```

### Tables and structured output

```go
var out format.Format
root.PersistentFlags().VarP(format.NewValue(&out, format.Text, nil), "output", "o",
	"output format: "+format.Default.String())

if out.IsStructured() {
	return format.Write(os.Stdout, out, items) // json tags drive JSON and YAML alike
}
t := table.New(os.Stdout, []string{"Name", "Status"},
	table.UppercaseHeaders(), table.WithStyle(table.Rounded), table.WithFormat(out))
for _, item := range items {
	t.Append(item.Name, item.Status)
}
return t.Render()
```

### Picking from a list

```go
i, err := pick.Select(os.Stdin, os.Stderr, pick.Options{
	Title:   "Deploy which version of %s?",
	Subject: "api",
	Items: []pick.Item{
		{Cells: []string{"1.4.2", "latest"}, Action: "deploy"},
		{Cells: []string{"1.4.1", "current"}, Muted: true},
	},
})
switch {
case errors.Is(err, pick.ErrNotATerminal):
	// Not interactive: ask for a --version flag instead.
case errors.Is(err, pick.ErrAborted):
	return nil // Esc or Ctrl-C
}

token, err := pick.Input(os.Stdin, os.Stderr, pick.InputOptions{Prompt: "Token: ", Secret: true})
```

Type to filter, arrows and paging keys to move, Enter to choose. Lists can
offer views (Tab), ranges, typed answers and per-column truncation; see the
package documentation. Text handed to a prompt is shown as plain text, so a
name from elsewhere cannot carry an escape sequence to the terminal, and the
terminal is restored when the prompt ends, also by a signal.

### Live output and progress

```go
defer live.InstallSignalRestore()() // never leave the cursor hidden

bar := progress.New(os.Stderr, progress.Options{})
logger := log.New(bar.Writer(os.Stderr), "", 0) // log lines step around the bar
bar.Start(len(steps), 30*time.Second)
for _, step := range steps {
	bar.Advance(step.Name)
	done := bar.BeginWait() // shows a countdown if the step stalls
	step.Run(logger)
	done()
}
bar.Finish(true)
bar.Clear()
```

The signal handler shows hidden cursors again before the process ends, and
runs whatever `live.OnRestore` registered, which is how the prompts of `pick`
and `secret` put the terminal back into its mode. The bar follows the width
of the terminal as it changes, holds a log line back until its newline
arrives, and uses 24-bit color only where `COLORTERM` announces it.

### Credentials

```go
files, _ := secret.NewFileStore("example")
r := &secret.Resolver{
	Name:    "API token",
	Account: "default",
	Env:     "EXAMPLE_TOKEN",
	Store:   secret.Fallback(secret.Keyring{Service: "example"}, files),
}
token, from, err := r.Resolve() // file flag, env, piped stdin, store, then a hidden prompt
// When the server accepts the token: r.Save(token).
// When it rejects it: r.Forget(from), then resolve again.
```

A Resolver reads a secret from stdin whenever stdin is not a terminal. In a
command that may run inside a loop reading stdin, in a Git hook or over ssh
without a terminal, set `StdinBusy` so that stdin is left alone.

### Self-update

```go
verifier, err := selfupdate.NewMinisign(publicKey) // "RWQ…", from minisign -G
u := &selfupdate.Updater{
	Source:   &selfupdate.GitHub{Owner: "example", Repo: "tool"},
	Current:  buildinfo.Get().Version,
	Asset:    selfupdate.VersionedAssets("tool"),
	Verifier: verifier, // checksums.txt must carry a valid checksums.txt.minisig
}
res, err := u.Update(ctx, false) // checks the archive against checksums.txt; refuses package-managed installs
```

The checksum detects a corrupted or altered download. On its own it does
not protect against someone who can write the release's files, because the
checksums file is published in the same place. A `Verifier` closes that
gap: the checksums file must be signed with a key that never touches the
release, such as a minisign key held in a CI secret. The package
documentation shows the GoReleaser `signs` entry. The archive must hold an
executable for the platform, and a credential is only sent to an `https`
API address.

A `.deb` install is refused by default. If the package comes from an APT
repository that is registered on the system, `Updater.APT` updates it through
APT instead. `Check` and `Update` then read the latest version from that
repository, so the `Source` and its token are not needed. The repository's
index and the package are fetched without root privileges into a temporary
directory. Only `apt-get update` and `apt-get install` of the downloaded file
run as root, through sudo when needed, so dependencies come from all of the
system's repositories. `Confirm` sees the exact commands first, so a command
can ask before it runs them, or skip the question for `--yes`:

```go
u.APT = &selfupdate.APT{
	Sources: "/etc/apt/sources.list.d/example.sources", // the file the package registers
	Confirm: func(ctx context.Context, plan selfupdate.APTPlan) (bool, error) {
		if yes {
			return true, nil
		}
		fmt.Fprintf(os.Stderr, "Update %s %s to %s with:\n%s\n", plan.Package, plan.Installed, plan.Version, plan)
		return askYesNo("Continue?") // the tool's own prompt
	},
}
```

APT verifies the index with the repository key, and the package against the
index.

### Files, browser and logs

```go
err := fsx.WriteFile(configPath, data, 0o644) // atomic; secret.WriteFile for a token cache

fmt.Fprintln(os.Stderr, "Opening", term.PlainLine(loginURL)) // say it, in case nothing opens
_ = browser.Open(ctx, loginURL)                               // http and https only

level := slog.LevelInfo
if verbose {
	level = slog.LevelDebug
}
logger := slog.New(logx.NewLineHandler(os.Stderr, &logx.Options{Level: level}))
logger.Debug("sent a request", "path", path, "status", status) // plain, credentials redacted
logger.Debug("response", "body", redact.Values(body, token))   // the token the program sent, in any escaped form
```

### Notices

```go
notice.Use(notice.New(notice.Options{
	Theme:  notice.Badge.WithColors(notice.Deep), // or Line, Bar, Block; Default, Muted, Pastel
	Debug:  verbose,
	Around: bar.WithCleared, // a progress bar steps aside for each notice
}))

notice.Warnf("Booking day %s was never left: the start at %s has no Gehen",
	notice.Date(day), notice.Time(start)) // values in their role's color
notice.Error(err)                       // plain and redacted, like every argument

mr := notice.NewRole("merge request", notice.RoleColors{Dark: theme.Fg256(218), Light: theme.Fg256(125)})
th := notice.Block.WithPatterns(notice.Pattern{Match: regexp.MustCompile(`[\w./-]+![0-9]+`), Role: mr})
```

### Shell completion and version

```go
cobrax.SetVersion(root, buildinfo.Get())
root.AddCommand(cobrax.NewVersionCommand(buildinfo.Get()))
cobrax.NoFileCompletions(root)
cobrax.AddCompletionInstall(cobrax.HardenCompletion(root)) // "completion bash --install"
cobrax.WriteCompletionScript(root, "zsh", legacyPath)      // where an older version kept it
root.SetArgs(cobrax.JoinSpacedBoolValues(root, os.Args[1:]))
```

Stamp the version at build time, for example in `.goreleaser.yaml`:

```yaml
ldflags:
  - -s -w
  - -X github.com/mittwald/cli-kit/buildinfo.Version={{ .Version }}
  - -X github.com/mittwald/cli-kit/buildinfo.Commit={{ .FullCommit }}
  - -X github.com/mittwald/cli-kit/buildinfo.Date={{ .Date }}
```

## Versioning

cli-kit follows [semantic versioning](https://semver.org). While it is
`v0.x`, minor releases may change APIs; such changes are listed first in the
release notes.

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for how to
build, test and submit changes, and [SECURITY.md](SECURITY.md) for reporting
vulnerabilities.

## License

Copyright (c) 2026 Mittwald CM Service GmbH & Co. KG and contributors

This project is licensed under the MIT License; see the [LICENSE](LICENSE)
file for details.
