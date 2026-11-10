package cobrax

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// SpacedBoolAnnotation is the flag annotation [AllowSpacedBool] sets and
// [JoinSpacedBoolValues] looks for.
const SpacedBoolAnnotation = "cobrax_spaced_bool"

// AllowSpacedBool lets the named boolean flags take their value after a
// space, as in "--stable false", once [JoinSpacedBoolValues] rewrites the
// arguments. It suits tri-state settings where clearing a flag is as normal
// as setting it. It panics when a flag is missing or not boolean.
func AllowSpacedBool(flags *pflag.FlagSet, names ...string) {
	for _, name := range names {
		flag := flags.Lookup(name)
		if flag == nil || flag.Value.Type() != "bool" {
			panic(fmt.Sprintf("cobrax: --%s is not a boolean flag", name))
		}
		if err := flags.SetAnnotation(name, SpacedBoolAnnotation, []string{"true"}); err != nil {
			panic(fmt.Sprintf("cobrax: annotate --%s: %v", name, err))
		}
	}
}

// JoinSpacedBoolValues rewrites "--flag false" into "--flag=false" for the
// boolean flags of root and its subcommands that [AllowSpacedBool] marked,
// so the space form parses. Pass the result to root.SetArgs.
//
// pflag gives a boolean flag NoOptDefVal "true", which lets "--flag" stand
// alone and leaves a following "false" behind as a positional argument.
// Only the marked long flags are joined, only when the next argument is
// exactly "true" or "false" (in any case), and never after "--". For a
// shell completion request the word being completed is left alone.
func JoinSpacedBoolValues(root *cobra.Command, args []string) []string {
	marked := map[string]bool{}
	walk(root, func(cmd *cobra.Command) {
		collect := func(flag *pflag.Flag) {
			if _, ok := flag.Annotations[SpacedBoolAnnotation]; ok {
				marked[flag.Name] = true
			}
		}
		cmd.LocalNonPersistentFlags().VisitAll(collect)
		cmd.PersistentFlags().VisitAll(collect)
	})
	if len(marked) == 0 {
		return args
	}

	limit := len(args)
	if len(args) > 0 && (args[0] == cobra.ShellCompRequestCmd || args[0] == cobra.ShellCompNoDescRequestCmd) {
		limit-- // The last word is the one being completed.
	}
	joined := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return append(joined, args[i:]...)
		}
		name, long := strings.CutPrefix(arg, "--")
		// limit never exceeds len(args); the second bound states it for
		// readers and for gosec.
		if long && marked[name] && i+1 < limit && i+1 < len(args) && isBoolLiteral(args[i+1]) {
			joined = append(joined, arg+"="+strings.ToLower(args[i+1]))
			i++
			continue
		}
		joined = append(joined, arg)
	}
	return joined
}

func isBoolLiteral(arg string) bool {
	switch strings.ToLower(arg) {
	case "true", "false":
		return true
	}
	return false
}
