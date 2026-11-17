package cobrax

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

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

// SafeCompletions returns the candidates of values that are safe to hand a
// shell, in their order, in a new slice. A candidate is cobra's
// "value\tdescription": a value, and optionally a tab and a description,
// as cobra.CompletionWithDesc builds it. That one tab is kept. A candidate
// is left out when its value is empty, when its value or its description
// holds a control character (C0, DEL or C1, among them a newline, a further
// tab or the ESC that starts an escape sequence), or when it is not valid
// UTF-8, whose stray bytes a terminal may read as C1 controls.
//
// Use it for candidates that come from elsewhere, such as names a remote
// service lists: the shell writes them to the terminal as they are, and a
// candidate holding a control character could not be completed to the name
// it stands for anyway. [CachedCompletion] and [CompletePrefix] apply it.
func SafeCompletions(values []cobra.Completion) []cobra.Completion {
	safe := make([]cobra.Completion, 0, len(values))
	for _, candidate := range values {
		if safeCompletion(candidate) {
			safe = append(safe, candidate)
		}
	}
	return safe
}

// safeCompletion reports whether a candidate is valid UTF-8, has a value,
// and holds no control character but the tab between its value and its
// description.
func safeCompletion(candidate string) bool {
	value, description, _ := strings.Cut(candidate, "\t")
	return value != "" && utf8.ValidString(candidate) &&
		!strings.ContainsFunc(value, unicode.IsControl) &&
		!strings.ContainsFunc(description, unicode.IsControl)
}

// CompletePrefix returns the candidates whose value starts with typed, the
// word being completed, for a completion function to return. Candidates may
// carry a description after a tab; only the value is matched. They go
// through [SafeCompletions] first.
//
// The directive switches off file names, and when every match ends in "=",
// such as a key that wants its value, it also tells the shell not to add a
// space after the completed word, so the value can be typed straight after
// it:
//
//	func(_ *cobra.Command, _ []string, typed string) ([]cobra.Completion, cobra.ShellCompDirective) {
//		return cobrax.CompletePrefix([]cobra.Completion{"name=", "status=", "limit="}, typed)
//	}
func CompletePrefix(candidates []cobra.Completion, typed string) ([]cobra.Completion, cobra.ShellCompDirective) {
	var matches []cobra.Completion
	open := true
	for _, candidate := range SafeCompletions(candidates) {
		value, _, _ := strings.Cut(candidate, "\t")
		if !strings.HasPrefix(value, typed) {
			continue
		}
		matches = append(matches, candidate)
		open = open && strings.HasSuffix(value, "=")
	}
	if open && len(matches) > 0 {
		return matches, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
	}
	return matches, cobra.ShellCompDirectiveNoFileComp
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
