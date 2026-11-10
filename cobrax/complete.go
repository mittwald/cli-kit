package cobrax

import (
	"fmt"
	"slices"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// CompleteValues returns a completion function that offers values and no
// file names. A value may carry a description after a tab, as
// cobra.CompletionWithDesc builds it.
func CompleteValues(values ...string) cobra.CompletionFunc {
	values = slices.Clone(values)
	return func(*cobra.Command, []string, string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return values, cobra.ShellCompDirectiveNoFileComp
	}
}

// NoFileCompletions switches off file-name completion for flags of cmd, which
// shells otherwise offer for any flag that takes a value. With names it
// covers those flags; without, every value-taking flag of cmd and of its
// subcommands that has no completion function of its own. Call it after all
// flags and completion functions are registered.
//
// It panics when a named flag does not exist.
func NoFileCompletions(cmd *cobra.Command, names ...string) {
	if len(names) > 0 {
		for _, name := range names {
			if cmd.Flag(name) == nil {
				panic(fmt.Sprintf("cobrax: no flag %q on %q", name, cmd.CommandPath()))
			}
			if _, ok := cmd.GetFlagCompletionFunc(name); !ok {
				mustRegister(cmd, name, cobra.NoFileCompletions)
			}
		}
		return
	}
	visit := func(c *cobra.Command) {
		register := func(flag *pflag.Flag) {
			if takesValue(flag) {
				if _, ok := c.GetFlagCompletionFunc(flag.Name); !ok {
					mustRegister(c, flag.Name, cobra.NoFileCompletions)
				}
			}
		}
		c.LocalNonPersistentFlags().VisitAll(register)
		c.PersistentFlags().VisitAll(register)
	}
	walk(cmd, visit)
}

// NoFileArgs switches off file-name completion for the positional arguments
// of cmd and its subcommands that complete nothing of their own: commands
// without ValidArgs or ValidArgsFunction.
func NoFileArgs(cmd *cobra.Command) {
	walk(cmd, func(c *cobra.Command) {
		if c.ValidArgsFunction == nil && len(c.ValidArgs) == 0 {
			c.ValidArgsFunction = cobra.NoFileCompletions
		}
	})
}

// takesValue reports whether flag needs a value, rather than being a switch
// such as a boolean flag.
func takesValue(flag *pflag.Flag) bool {
	return flag.NoOptDefVal == "" && flag.Value.Type() != "bool"
}

// walk calls visit for cmd and every command below it.
func walk(cmd *cobra.Command, visit func(*cobra.Command)) {
	visit(cmd)
	for _, child := range cmd.Commands() {
		walk(child, visit)
	}
}

// mustRegister registers a flag completion function and panics on failure,
// which only a missing or doubly registered flag causes.
func mustRegister(cmd *cobra.Command, name string, complete cobra.CompletionFunc) {
	if err := cmd.RegisterFlagCompletionFunc(name, complete); err != nil {
		panic(fmt.Sprintf("cobrax: register completion for --%s: %v", name, err))
	}
}
