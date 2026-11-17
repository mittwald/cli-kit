package cobrax

import (
	"github.com/spf13/cobra"

	"github.com/mittwald/cli-kit/term"
)

// ModeValue is a pflag.Value for a flag that takes auto, always or never,
// such as --links[=WHEN] or --pager[=WHEN]. Unlike a [ColorValue] it
// records nothing for the process: the program asks [ModeValue.Mode] and
// decides, for a link with term.ShouldLink, for example.
//
// The zero ModeValue reads values as [AddModeFlag] without options does.
type ModeValue struct {
	mode   term.ColorMode
	parser modeParser
}

// Set reads a value with term.ParseColorMode, or only auto, always and
// never under [StrictMode], and records it. An empty value and "true", the
// spellings of a bare flag, mean the mode [BareMode] chose,
// [term.ColorAlways] unless it chose another. A value that is not accepted
// is an error, so the command line is rejected as it is parsed.
func (v *ModeValue) Set(value string) error {
	mode, err := v.parser.parse(value, "value")
	if err != nil {
		return err
	}
	v.mode = mode
	return nil
}

// String returns the current mode's flag spelling.
func (v *ModeValue) String() string { return v.mode.String() }

// Type names the value in help output.
func (v *ModeValue) Type() string { return "when" }

// IsBoolFlag lets the flag stand alone when it is used with the standard
// library's flag package. pflag reads NoOptDefVal instead, which
// [AddModeFlag] sets.
func (v *ModeValue) IsBoolFlag() bool { return true }

// Mode returns the mode the flag was last set to, [term.ColorAuto] when it
// was not given.
func (v *ModeValue) Mode() term.ColorMode { return v.mode }

// ModeOption changes how a flag registered with [AddModeFlag] reads its
// value. The options are those of [AddColorFlagWith], for a flag of any
// name.
type ModeOption func(*modeFlag)

// modeFlag is what the options of one mode flag set.
type modeFlag struct {
	value   *ModeValue
	aliases []string
}

// ModeAliases registers the flag under further names, all sharing one
// value.
func ModeAliases(names ...string) ModeOption {
	return func(f *modeFlag) { f.aliases = append(f.aliases, names...) }
}

// BareMode sets the mode the bare flag means, and with it --name= and
// --name=true, the other spellings of a bare flag. The default is
// [term.ColorAlways].
func BareMode(mode term.ColorMode) ModeOption {
	return func(f *modeFlag) { f.value.parser.bare = &mode }
}

// StrictMode accepts auto, always and never alone, in any case, besides the
// spellings of a bare flag. The other spellings term.ParseColorMode reads,
// such as yes, none or tty, are rejected.
func StrictMode() ModeOption {
	return func(f *modeFlag) { f.value.parser.strict = true }
}

// ModeError words the error for a value the flag rejects: invalid is called
// with the value as given and returns the error. pflag still puts its own
// words in front, naming the value and the flag.
func ModeError(invalid func(value string) error) ModeOption {
	return func(f *modeFlag) { f.value.parser.invalid = invalid }
}

// AddModeFlag registers --name[=WHEN] as a persistent flag of cmd: a flag
// that takes auto, always or never, as --color does, for a choice other
// than color. A bare --name means always unless [BareMode] says otherwise,
// a value that is not accepted fails the command line as it is parsed, and
// completion offers auto, always and never. The flag is auto until it is
// given.
//
// A tool that makes links of its table's first column registers
//
//	links := cobrax.AddModeFlag(root, "links", "when to make links: auto, always or never")
//
// and asks term.ShouldLink(links.Mode(), os.Stdout) before it writes one.
//
// AddModeFlag panics when a flag of that name is already defined, as pflag
// does for any redefined flag.
func AddModeFlag(cmd *cobra.Command, name, usage string, opts ...ModeOption) *ModeValue {
	f := modeFlag{value: &ModeValue{}}
	for _, opt := range opts {
		opt(&f)
	}
	for _, flagName := range append([]string{name}, f.aliases...) {
		cmd.PersistentFlags().Var(f.value, flagName, usage)
		cmd.PersistentFlags().Lookup(flagName).NoOptDefVal = f.value.parser.bareMode().String()
		mustRegister(cmd, flagName, CompleteValues(term.ColorModeNames...))
	}
	return f.value
}
