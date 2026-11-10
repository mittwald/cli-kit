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

cli-kit supports Go 1.24 and newer on Linux, macOS and Windows. It is one
module: import only the packages you need, and only their dependencies are
compiled into your binary.

## Packages

| Package | What it does |
|---|---|
| [`term`](https://pkg.go.dev/github.com/mittwald/cli-kit/term) | Terminal detection and size, the `--color` mode with `NO_COLOR` and `TERM=dumb`, visible text width, truncation, padding, OSC 8 hyperlinks |
| [`theme`](https://pkg.go.dev/github.com/mittwald/cli-kit/theme) | SGR styles and the color roles the other packages paint with |
| [`pick`](https://pkg.go.dev/github.com/mittwald/cli-kit/pick) | Interactive list selection with filtering, views and ranges, switches, and line-edited text and secret input |
| [`live`](https://pkg.go.dev/github.com/mittwald/cli-kit/live) | Blocks of lines redrawn in place, detail/result output separation, and cursor restore on exit, panic and signals |
| [`progress`](https://pkg.go.dev/github.com/mittwald/cli-kit/progress) | A gradient progress bar with a stall countdown that steps aside for log lines |
| [`table`](https://pkg.go.dev/github.com/mittwald/cli-kit/table) | Tables in plain, framed and Markdown styles, and CSV/TSV records |
| [`format`](https://pkg.go.dev/github.com/mittwald/cli-kit/format) | An `--output` flag value and JSON/YAML writers driven by the same struct tags |
| [`ics`](https://pkg.go.dev/github.com/mittwald/cli-kit/ics) | An RFC 5545 iCalendar writer |
| [`secret`](https://pkg.go.dev/github.com/mittwald/cli-kit/secret) | Credentials from flags, files, environment, stdin, the OS keyring or a hidden prompt |
| [`selfupdate`](https://pkg.go.dev/github.com/mittwald/cli-kit/selfupdate) | Checksum-verified self-updates from GitHub Releases or a GitLab package registry |
| [`cobrax`](https://pkg.go.dev/github.com/mittwald/cli-kit/cobrax) | Cobra helpers: `--color`, enum flags, hardened and cached shell completion, version command |
| [`buildinfo`](https://pkg.go.dev/github.com/mittwald/cli-kit/buildinfo) | Version, commit and date from `-ldflags` or the Go build information |

## Examples

### Color and terminal

```go
cobrax.AddColorFlag(root, "when to use color: auto, always or never")

if term.Colored(os.Stdout) {
	fmt.Println(theme.Active().Success.Paint("done"))
}
width := term.Columns(os.Stdout, 80)
fmt.Println(term.Truncate(longLine, width))
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
package documentation.

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

### Credentials

```go
files, _ := secret.NewFileStore("example")
r := &secret.Resolver{
	Label:   "API token",
	Account: "default",
	Env:     "EXAMPLE_TOKEN",
	Store:   secret.Fallback(secret.Keyring{Service: "example"}, files),
}
token, from, err := r.Resolve() // file flag, env, piped stdin, store, then a hidden prompt
// When the server accepts the token: r.Save(token).
// When it rejects it: r.Forget(from), then resolve again.
```

### Self-update

```go
u := &selfupdate.Updater{
	Source:  &selfupdate.GitHub{Owner: "example", Repo: "tool"},
	Current: buildinfo.Get().Version,
	Asset:   selfupdate.VersionedAssets("tool"),
}
res, err := u.Update(ctx, false) // verifies checksums.txt; refuses package-managed installs
```

### Shell completion and version

```go
cobrax.SetVersion(root, buildinfo.Get())
root.AddCommand(cobrax.NewVersionCommand(buildinfo.Get()))
cobrax.NoFileCompletions(root)
cobrax.HardenCompletion(root)
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
