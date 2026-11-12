package cobrax

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mittwald/cli-kit/term"
)

// ColorValue is a pflag.Value for --color[=WHEN]. Setting it records the mode
// for the whole process with term.SetColorMode, so every package that asks
// term.Colored follows the flag.
//
// The zero ColorValue reads values as [AddColorFlag] does; [AddColorFlagWith]
// and its options change that.
type ColorValue struct {
	mode    term.ColorMode
	bare    *term.ColorMode
	strict  bool
	invalid func(value string) error
}

// Set parses a --color value with term.ParseColorMode and records it. An
// empty value and "true", the spellings of a bare --color, mean the mode
// [BareColor] chose, [term.ColorAlways] unless it chose another.
func (v *ColorValue) Set(value string) error {
	mode, err := v.parse(value)
	if err != nil {
		if v.invalid != nil {
			return v.invalid(value)
		}
		return err
	}
	v.mode = mode
	term.SetColorMode(mode)
	return nil
}

// parse reads value as Set describes.
func (v *ColorValue) parse(value string) (term.ColorMode, error) {
	name := strings.ToLower(strings.TrimSpace(value))
	if name == "" || name == "true" {
		return v.bareMode(), nil
	}
	if v.strict && !slices.Contains(term.ColorModeNames, name) {
		return term.ColorAuto, fmt.Errorf("invalid color mode %q: want one of %s", value, strings.Join(term.ColorModeNames, ", "))
	}
	return term.ParseColorMode(value)
}

// bareMode is the mode a bare --color means.
func (v *ColorValue) bareMode() term.ColorMode {
	if v.bare == nil {
		return term.ColorAlways
	}
	return *v.bare
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
// aliases (such as the British spelling), all sharing one value. A bare
// --color means "always", the way ls reads it; [AddColorFlagWith] and
// [BareColor] give the grep reading, "auto". Values are read with
// term.ParseColorMode. Completion offers auto, always and never.
//
// AddColorFlag panics when a flag of that name is already defined, as pflag
// does for any redefined flag.
func AddColorFlag(cmd *cobra.Command, usage string, aliases ...string) *ColorValue {
	return AddColorFlagWith(cmd, usage, ColorAliases(aliases...))
}

// ColorOption changes how a --color flag registered with [AddColorFlagWith]
// reads its value.
type ColorOption func(*colorFlag)

// colorFlag is what the options of one --color flag set.
type colorFlag struct {
	value   *ColorValue
	aliases []string
}

// ColorAliases registers the flag under further names, such as the British
// spelling, all sharing one value.
func ColorAliases(names ...string) ColorOption {
	return func(f *colorFlag) { f.aliases = append(f.aliases, names...) }
}

// BareColor sets the mode a bare --color means, and with it --color= and
// --color=true, the other spellings of a bare flag. The default is
// [term.ColorAlways], the way ls reads a bare --color; [term.ColorAuto] is
// the way grep reads it.
func BareColor(mode term.ColorMode) ColorOption {
	return func(f *colorFlag) { f.value.bare = &mode }
}

// StrictColor accepts auto, always and never alone, in any case, besides the
// spellings of a bare --color. The other spellings term.ParseColorMode
// reads, such as yes, none or tty, are rejected.
func StrictColor() ColorOption {
	return func(f *colorFlag) { f.value.strict = true }
}

// ColorError words the error for a value the flag rejects: invalid is
// called with the value as given and returns the error. pflag still puts
// its own words in front, naming the value and the flag.
func ColorError(invalid func(value string) error) ColorOption {
	return func(f *colorFlag) { f.value.invalid = invalid }
}

// AddColorFlagWith is [AddColorFlag] with options: aliases, the reading of a
// bare --color, which values are accepted, and the wording of the error. A
// tool that reads a bare --color as auto and takes nothing but auto, always
// and never registers its flag as
//
//	cobrax.AddColorFlagWith(root, "when to use color: auto, always or never",
//		cobrax.BareColor(term.ColorAuto), cobrax.StrictColor())
//
// Without options it registers the flag AddColorFlag does, and it panics in
// the same case.
func AddColorFlagWith(cmd *cobra.Command, usage string, opts ...ColorOption) *ColorValue {
	f := colorFlag{value: &ColorValue{}}
	for _, opt := range opts {
		opt(&f)
	}
	for _, name := range append([]string{"color"}, f.aliases...) {
		cmd.PersistentFlags().Var(f.value, name, usage)
		cmd.PersistentFlags().Lookup(name).NoOptDefVal = f.value.bareMode().String()
		mustRegister(cmd, name, CompleteValues(term.ColorModeNames...))
	}
	return f.value
}
