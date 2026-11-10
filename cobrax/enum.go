package cobrax

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Enum is a pflag.Value that accepts one of a fixed set of strings, such as
// the formats an --output flag offers. Matching ignores case; the value
// keeps the allowed spelling.
type Enum struct {
	value    string
	allowed  []string
	typeName string
}

// NewEnum returns an Enum holding value and accepting the allowed strings.
// value need not be one of them, so an empty default can mean "not given".
func NewEnum(value string, allowed ...string) *Enum {
	return &Enum{value: value, allowed: slices.Clone(allowed), typeName: "string"}
}

// WithType sets the name help output shows for the value, such as "format".
// It returns e for chaining.
func (e *Enum) WithType(name string) *Enum {
	e.typeName = name
	return e
}

// Set accepts value when it is one of the allowed strings.
func (e *Enum) Set(value string) error {
	for _, allowed := range e.allowed {
		if strings.EqualFold(value, allowed) {
			e.value = allowed
			return nil
		}
	}
	return fmt.Errorf("invalid value %q: want one of %s", value, strings.Join(e.allowed, ", "))
}

// String returns the current value.
func (e *Enum) String() string { return e.value }

// Type names the value in help output.
func (e *Enum) Type() string { return e.typeName }

// Value returns the current value.
func (e *Enum) Value() string { return e.value }

// Allowed returns the accepted strings, in the order they were given.
func (e *Enum) Allowed() []string { return slices.Clone(e.allowed) }

// Complete is a cobra completion function offering the allowed strings.
func (e *Enum) Complete(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	return CompleteValues(e.allowed...)(cmd, args, toComplete)
}

// AddEnumFlag defines an [Enum] flag on flags, which is cmd.Flags() or
// cmd.PersistentFlags(), and registers its completion on cmd. shorthand may
// be empty. It panics when the flag is already defined, as pflag does.
func AddEnumFlag(cmd *cobra.Command, flags *pflag.FlagSet, name, shorthand, value, usage string, allowed ...string) *Enum {
	enum := NewEnum(value, allowed...)
	flags.VarP(enum, name, shorthand, usage)
	mustRegister(cmd, name, enum.Complete)
	return enum
}
