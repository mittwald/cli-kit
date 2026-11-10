# Contributing to cli-kit

Thanks for helping! cli-kit collects the terminal and command-line plumbing
that our Go tools kept writing again: terminal detection, prompts, tables,
progress output, secrets and self-updates. Contributions that make these
pieces more robust or more broadly useful are welcome.

## Getting started

You need Go 1.24 or newer and [golangci-lint](https://golangci-lint.run) v2.

```shell
git clone https://github.com/mittwald/cli-kit.git
cd cli-kit
make check      # vet, lint, race tests, cross-compile, go mod tidy check
make test-min   # run the tests on Go 1.24, the oldest supported release
```

Interactive packages (`pick`, `live`, `progress`) are tested without a
terminal. To try them by hand, write a small `main` package in a scratch
directory with a `replace github.com/mittwald/cli-kit => ../cli-kit`
directive.

## Ground rules

- **One module.** All packages share one `go.mod` and one version. A package
  moves into a module of its own only when it needs a heavy or cgo
  dependency, or has to version independently. Discuss that in an issue
  first.
- **Few dependencies.** Every dependency is paid for by every user of the
  module. Prefer the standard library and `golang.org/x`. Explain any new
  dependency in the pull request.
- **Go 1.24 compatibility.** Do not use standard-library APIs newer than the
  `go` directive in `go.mod`. CI runs the tests on Go 1.24 to catch this.
- **Platforms.** Everything must build on Linux, macOS and Windows. Features
  that only work on Unix degrade gracefully elsewhere and say so in their
  documentation.
- **No hidden global state.** Configuration is passed in as options. The few
  process-wide settings (`term.SetColorMode`, `theme.Use`) are documented as
  such.
- **Plain output when not on a terminal.** Anything that writes escape
  sequences asks `term.Colored` or `term.CanAnimate` first, so pipes, files
  and test buffers stay clean.
- **Never print secrets.** Errors and logs must not contain credential
  values.

## Code style

- Run `make fmt` and `make lint` before you push.
- Every exported identifier has a godoc comment that ends with a period.
  Each package has its overview in `doc.go`.
- Write comments and documentation in US English.
- Add tests for new behavior. Add an `Example` function where the output is
  deterministic: it documents the package and is tested at the same time.

## Pull requests

- Keep a pull request to one topic.
- Use [Conventional Commits](https://www.conventionalcommits.org) for commit
  messages, for example `feat(table): add a compact border style` or
  `fix(pick): keep the cursor on the selected row after filtering`.
- Mark breaking API changes with `!` (`feat(pick)!: …`) and the
  `breaking-change` label. cli-kit is `v0.x`, so breaking changes are
  allowed, but they bump the minor version and are listed first in the
  release notes.

## Releases

Maintainers tag releases from `master` as `vMAJOR.MINOR.PATCH`. Pushing the
tag runs the release workflow: it tests the tagged commit, creates the
GitHub release with generated notes, and asks the Go module proxy to fetch
the version so it appears on pkg.go.dev. Tags are never moved or deleted;
a broken release is withdrawn with a `retract` directive in `go.mod`.

## Reporting security issues

Please do not open public issues for vulnerabilities. See
[SECURITY.md](SECURITY.md).
