// Command demo shows the visual packages of cli-kit at work. Run it from the
// module root:
//
//	go run ./examples/demo <command>
//
// The commands are:
//
//   - pick: choose a release from a list with views and muted rows, then
//     type a text answer and a hidden secret (needs a terminal)
//   - progress: a progress bar over fake steps, with log lines that step
//     around it and a stalled step that counts down its timeout
//   - live: a block of lines redrawn in place
//   - table: a table in every drawn style, or in the format --output names
//   - theme: the color roles, a painted diff, painted JSON, a stable color
//     per name and a style read from --style
//   - term: hyperlinks, truncation, wrapping and hostile text made safe
//   - version: the version, commit and build date
//   - completion: shell completion scripts, with --install for bash, zsh
//     and fish
//
// Every command keeps its output plain when it does not go to a terminal,
// so piping it through cat shows what a script would read. --color and
// --links take auto, always or never.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mittwald/cli-kit/buildinfo"
	"github.com/mittwald/cli-kit/cobrax"
	"github.com/mittwald/cli-kit/live"
)

func main() {
	os.Exit(run())
}

// run builds and executes the command tree. It is separate from main so its
// deferred calls run before the process exits.
func run() int {
	// Never leave the cursor hidden or the terminal in raw mode, also when
	// the demo is interrupted.
	defer live.InstallSignalRestore()()

	root := &cobra.Command{
		Use:           "demo",
		Short:         "Show the visual packages of cli-kit at work",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cobrax.AddColorFlag(root, "when to use color: auto, always or never")
	links := cobrax.AddModeFlag(root, "links", "when to make links: auto, always or never")

	info := buildinfo.Get()
	cobrax.SetVersion(root, info)
	root.AddCommand(
		newPickCommand(),
		newProgressCommand(),
		newLiveCommand(),
		newTableCommand(),
		newThemeCommand(),
		newTermCommand(links),
		cobrax.NewVersionCommand(info),
	)
	cobrax.NoFileArgs(root)
	// The completion command exists only once the subcommands are added.
	cobrax.AddCompletionInstall(cobrax.HardenCompletion(root))

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		return 1
	}
	return 0
}
