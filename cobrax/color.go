package cobrax

import (
	"github.com/spf13/cobra"

	"github.com/mittwald/cli-kit/term"
)

// ColorValue is a pflag.Value for --color[=WHEN]. Setting it records the mode
// for the whole process with term.SetColorMode, so every package that asks
// term.Colored follows the flag.
type ColorValue struct {
	mode term.ColorMode
}

// Set parses a --color value with term.ParseColorMode and records it.
func (v *ColorValue) Set(value string) error {
	mode, err := term.ParseColorMode(value)
	if err != nil {
		return err
	}
	v.mode = mode
	term.SetColorMode(mode)
	return nil
}

// String returns the current mode's flag spelling.
func (v *ColorValue) String() string { return v.mode.String() }

// Type names the value in help output.
func (v *ColorValue) Type() string { return "when" }

// IsBoolFlag lets the flag stand alone, as a bare --color, when it is used
// with the standard library's flag package. pflag reads NoOptDefVal instead,
// which [AddColorFlag] sets.
func (v *ColorValue) IsBoolFlag() bool { return true }

// Mode returns the mode the flag was last set to, [term.ColorAuto] when it
// was not given.
func (v *ColorValue) Mode() term.ColorMode { return v.mode }

// AddColorFlag registers --color[=WHEN] as a persistent flag of cmd, plus any
// aliases (such as the British spelling), all sharing one value. A bare --color means
// "always", the way ls reads it; set the flag's NoOptDefVal to "auto" for the
// grep reading. Completion offers auto, always and never.
//
// AddColorFlag panics when a flag of that name is already defined, as pflag
// does for any redefined flag.
func AddColorFlag(cmd *cobra.Command, usage string, aliases ...string) *ColorValue {
	value := &ColorValue{}
	for _, name := range append([]string{"color"}, aliases...) {
		cmd.PersistentFlags().Var(value, name, usage)
		cmd.PersistentFlags().Lookup(name).NoOptDefVal = term.ColorAlways.String()
		mustRegister(cmd, name, CompleteValues(term.ColorModeNames...))
	}
	return value
}
