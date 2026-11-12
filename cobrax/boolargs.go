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
// boolean flags that [AllowSpacedBool] marked, so the space form parses.
// Pass the result to root.SetArgs.
//
// pflag gives a boolean flag NoOptDefVal "true", which lets "--flag" stand
// alone and leaves a following "false" behind as a positional argument.
// Only long flags are joined, and only when the command the arguments
// select has a marked flag of that name, its own or inherited: an unmarked
// flag of the same name on another command keeps its meaning. The next
// argument must be exactly "true" or "false" (in any case), and nothing
// after "--" is touched, nor is the value of another flag: in
// "--name --stable false" the flag --name is given the value "--stable". For
// a shell completion request the word being completed is left alone.
func JoinSpacedBoolValues(root *cobra.Command, args []string) []string {
	anyMarked := false
	walk(root, func(cmd *cobra.Command) {
		cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
			anyMarked = anyMarked || spaced(flag)
		})
	})
	if !anyMarked {
		return args
	}

	first, limit := 0, len(args)
	if len(args) > 0 && (args[0] == cobra.ShellCompRequestCmd || args[0] == cobra.ShellCompNoDescRequestCmd) {
		if len(args) < 3 {
			return args // At most the word being completed: nothing to join.
		}
		first, limit = 1, limit-1 // The last word is the one being completed.
	}
	cmd := selected(root, args[first:limit])
	joined := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return append(joined, args[i:]...)
		}
		name, long := strings.CutPrefix(arg, "--")
		// limit never exceeds len(args); the second bound states it for
		// readers and for gosec.
		if long && i >= first && i+1 < limit && i+1 < len(args) && isBoolLiteral(args[i+1]) && spaced(lookupFlag(cmd, name)) {
			joined = append(joined, arg+"="+strings.ToLower(args[i+1]))
			i++
			continue
		}
		joined = append(joined, arg)
		// The value of a flag is never a flag itself: "--name --stable false"
		// names something "--stable", and joining would rename it.
		if i >= first && i+1 < len(args) && takesNext(cmd, arg) {
			i++
			joined = append(joined, args[i])
		}
	}
	return joined
}

// takesNext reports whether arg is a flag of cmd that takes the argument
// after it as its value: "--name" or "-n" of a flag that needs a value, or a
// group of short flags ending in one ("-vn").
func takesNext(cmd *cobra.Command, arg string) bool {
	if strings.Contains(arg, "=") {
		return false
	}
	if name, long := strings.CutPrefix(arg, "--"); long {
		flag := lookupFlag(cmd, name)
		return flag != nil && flag.NoOptDefVal == ""
	}
	if len(arg) < 2 || arg[0] != '-' {
		return false
	}
	for i := 1; i < len(arg); i++ {
		flag := lookupShorthand(cmd, arg[i:i+1])
		if flag == nil {
			return false
		}
		if flag.NoOptDefVal == "" {
			// The rest of the group is this flag's value, unless the flag
			// ends the group.
			return i == len(arg)-1
		}
	}
	return false
}

// selected returns the command args select, the way cobra's Find walks
// them, except that "true" or "false" after a marked flag is that flag's
// value rather than a command name.
func selected(root *cobra.Command, args []string) *cobra.Command {
	cmd := root
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			return cmd
		case strings.HasPrefix(arg, "--") && !strings.Contains(arg, "="):
			flag := lookupFlag(cmd, arg[2:])
			// As in cobra, a flag that is unknown here or needs a value takes
			// the next argument.
			if flag == nil || flag.NoOptDefVal == "" || spaced(flag) && i+1 < len(args) && isBoolLiteral(args[i+1]) {
				i++
			}
		case len(arg) == 2 && arg[0] == '-' && arg[1] != '-':
			if flag := lookupShorthand(cmd, arg[1:]); flag == nil || flag.NoOptDefVal == "" {
				i++
			}
		case strings.HasPrefix(arg, "-"):
			// Joined short flags and --flag=value carry their values.
		default:
			child, _, _ := cmd.Find([]string{arg})
			if child == nil || child == cmd {
				return cmd // The first positional argument ends the walk.
			}
			cmd = child
		}
	}
	return cmd
}

// lookupFlag finds a long flag of cmd, its own or inherited from a parent.
func lookupFlag(cmd *cobra.Command, name string) *pflag.Flag {
	if flag := cmd.LocalFlags().Lookup(name); flag != nil {
		return flag
	}
	return cmd.InheritedFlags().Lookup(name)
}

func lookupShorthand(cmd *cobra.Command, name string) *pflag.Flag {
	if flag := cmd.LocalFlags().ShorthandLookup(name); flag != nil {
		return flag
	}
	return cmd.InheritedFlags().ShorthandLookup(name)
}

// spaced reports whether [AllowSpacedBool] marked flag; nil is not marked.
func spaced(flag *pflag.Flag) bool {
	if flag == nil {
		return false
	}
	_, ok := flag.Annotations[SpacedBoolAnnotation]
	return ok
}

func isBoolLiteral(arg string) bool {
	switch strings.ToLower(arg) {
	case "true", "false":
		return true
	}
	return false
}
