package cobrax

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// ScriptError reports that writing a completion script failed, as opposed
// to the command line being wrong. A caller that prints usage for bad input
// can skip it for this error: the command was right, its output failed.
type ScriptError struct {
	// Shell is the shell the script was generated for.
	Shell string
	// Err is the underlying write error.
	Err error
}

// Error describes the failure.
func (e *ScriptError) Error() string {
	return fmt.Sprintf("write %s completion script: %v", e.Shell, e.Err)
}

// Unwrap returns the underlying error.
func (e *ScriptError) Unwrap() error { return e.Err }

// HardenCompletion adds cobra's default completion command to root now,
// rather than when the command runs, and fixes three of its rough edges:
//
//   - "completion" with an unknown shell ("completion tcsh") fails instead of
//     printing help with a successful exit status;
//   - scripts are written to the command's output writer at the time they
//     are generated, so SetOut called after HardenCompletion still applies;
//   - a failure to write a script is returned as a [*ScriptError].
//
// Call it after every subcommand is added: cobra only adds the completion
// command to a root that has subcommands. It returns the completion command,
// or nil when there is none (no subcommands, or
// root.CompletionOptions.DisableDefaultCmd set).
func HardenCompletion(root *cobra.Command) *cobra.Command {
	root.InitDefaultCompletionCmd()
	var completion *cobra.Command
	for _, cmd := range root.Commands() {
		if cmd.Name() == "completion" {
			completion = cmd
			break
		}
	}
	if completion == nil {
		return nil
	}
	// A runnable command has its arguments validated, and cobra declares the
	// completion command with cobra.NoArgs, so an unknown shell name now
	// fails as an unknown command.
	completion.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
	for _, shell := range completion.Commands() {
		name := shell.Name()
		generate := generator(root, name)
		fallback := shell.RunE
		shell.RunE = func(cmd *cobra.Command, args []string) error {
			var err error
			switch {
			case generate != nil:
				err = generate(cmd, cmd.OutOrStdout())
			case fallback != nil:
				err = fallback(cmd, args)
			}
			if err != nil {
				return &ScriptError{Shell: name, Err: err}
			}
			return nil
		}
	}
	return completion
}

// generator returns the script writer for one of the shells cobra supports,
// or nil for a shell it does not know.
func generator(root *cobra.Command, shell string) func(*cobra.Command, io.Writer) error {
	describe := func(cmd *cobra.Command) bool {
		if root.CompletionOptions.DisableDescriptions {
			return false
		}
		noDescriptions, err := cmd.Flags().GetBool("no-descriptions")
		return err != nil || !noDescriptions
	}
	switch shell {
	case "bash":
		return func(cmd *cobra.Command, w io.Writer) error {
			return root.GenBashCompletionV2(w, describe(cmd))
		}
	case "zsh":
		return func(cmd *cobra.Command, w io.Writer) error {
			if describe(cmd) {
				return root.GenZshCompletion(w)
			}
			return root.GenZshCompletionNoDesc(w)
		}
	case "fish":
		return func(cmd *cobra.Command, w io.Writer) error {
			return root.GenFishCompletion(w, describe(cmd))
		}
	case "powershell":
		return func(cmd *cobra.Command, w io.Writer) error {
			if describe(cmd) {
				return root.GenPowerShellCompletionWithDesc(w)
			}
			return root.GenPowerShellCompletion(w)
		}
	}
	return nil
}
