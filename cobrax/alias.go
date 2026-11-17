package cobrax

import (
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// FlagAlias is an older spelling of a flag: a switch that stands for a flag
// with a value, as --no-colors stands for --color=never. [AddFlagAlias]
// registers it.
type FlagAlias struct {
	// Old is the name of the switch, without dashes.
	Old string
	// New is the name of the flag it stands for, without dashes.
	New string
	// Value is the value it gives that flag.
	Value string
	// Silent keeps the user from being told about the replacement, for a
	// spelling that is as good as the new one.
	Silent bool
}

// replacement is how the deprecation note spells the flag to use instead.
func (a FlagAlias) replacement() string {
	if a.Value == "" {
		return "--" + a.New
	}
	return "--" + a.New + "=" + a.Value
}

// AddFlagAlias registers alias.Old as a hidden switch on flags, which is
// cmd.Flags() or cmd.PersistentFlags() and must hold the flag alias.New.
// Giving the switch sets alias.New to alias.Value, as if
// --New=Value had been given, so the value is checked as that flag checks
// it and the flag counts as changed. --Old=false is accepted and does
// nothing, and any other value than a boolean is an error.
//
// Unless alias.Silent is set, the first use of the switch writes a note to
// the error stream of cmd, cmd.ErrOrStderr():
//
//	WARNING: --no-colors is deprecated, use --color=never
//
// The note is written once per registration, also when the switch is given
// twice. The switch is left out of help and of completion, so it keeps
// working without being offered.
//
// AddFlagAlias panics when alias.New is not a flag of flags, and when a flag
// named alias.Old is already defined, as pflag does for any redefined flag.
func AddFlagAlias(cmd *cobra.Command, flags *pflag.FlagSet, alias FlagAlias) {
	if alias.Old == "" || flags.Lookup(alias.New) == nil {
		panic(fmt.Sprintf("cobrax: alias %q for a missing flag %q on %q", alias.Old, alias.New, cmd.CommandPath()))
	}
	value := &aliasValue{alias: alias, flags: flags, stderr: cmd.ErrOrStderr}
	flags.Var(value, alias.Old, "same as "+alias.replacement())
	flag := flags.Lookup(alias.Old)
	flag.NoOptDefVal = "true"
	flag.Hidden = true
}

// aliasValue is the pflag.Value of an alias switch.
type aliasValue struct {
	alias  FlagAlias
	flags  *pflag.FlagSet
	stderr func() io.Writer
	told   sync.Once
	given  bool
}

// Set reads the switch's value as a boolean and, when it is true, sets the
// flag it stands for.
func (v *aliasValue) Set(value string) error {
	on, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("invalid value %q: want true or false", value)
	}
	if !v.alias.Silent {
		v.told.Do(func() {
			_, _ = fmt.Fprintf(v.stderr(), "WARNING: --%s is deprecated, use %s\n", v.alias.Old, v.alias.replacement())
		})
	}
	if !on {
		return nil
	}
	v.given = true
	return v.flags.Set(v.alias.New, v.alias.Value)
}

// String reports whether the switch was given.
func (v *aliasValue) String() string { return strconv.FormatBool(v.given) }

// Type is "bool", so the switch is read as one.
func (v *aliasValue) Type() string { return "bool" }

// IsBoolFlag lets the switch stand alone with the standard library's flag
// package.
func (v *aliasValue) IsBoolFlag() bool { return true }
