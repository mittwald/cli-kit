package format

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/mittwald/cli-kit/term"
)

// Format is the name of an output format, as given to an --output flag.
type Format string

// The formats this package classifies.
const (
	// Text is human-readable output, usually a drawn table. It is the only
	// format that may be painted.
	Text Format = "text"
	// JSON is indented JSON.
	JSON Format = "json"
	// YAML is block-style YAML carrying the same view as JSON.
	YAML Format = "yaml"
	// CSV is comma-separated records (RFC 4180).
	CSV Format = "csv"
	// TSV is tab-separated records, never quoted.
	TSV Format = "tsv"
	// Markdown is a GitHub-flavored pipe table.
	Markdown Format = "markdown"
)

// String returns the flag spelling of f.
func (f Format) String() string { return string(f) }

// IsStructured reports whether f is a format for programs to read (JSON or
// YAML), which carries the data and none of the prose a text view adds.
func (f Format) IsStructured() bool { return f == JSON || f == YAML }

// IsDelimited reports whether f is a record format (CSV or TSV): a header
// record and one record per row, and nothing else.
func (f Format) IsDelimited() bool { return f == CSV || f == TSV }

// IsVerbatim reports whether f is text another program or document takes as
// it stands (markdown, CSV, TSV). Such output must never carry escape
// sequences, even under --color=always, and should carry the table alone.
func (f Format) IsVerbatim() bool { return f == Markdown || f.IsDelimited() }

// Colored reports whether output in format f written to w may be painted:
// only text output is, and only when term.Colored allows it for w. The empty
// Format counts as text.
func (f Format) Colored(w io.Writer) bool {
	return (f == Text || f == "") && term.Colored(w)
}

// Set is the list of formats a command accepts, in the order help text and
// shell completion list them.
type Set []Format

// Default is every format this package classifies, text first.
var Default = Set{Text, JSON, YAML, CSV, TSV, Markdown}

// aliases are alternative spellings Parse accepts for a format in the set.
var aliases = map[string]Format{
	"yml":   YAML,
	"md":    Markdown,
	"table": Text,
	"txt":   Text,
}

// Parse reads value as one of the formats in [Default]. See [Set.Parse].
func Parse(value string) (Format, error) { return Default.Parse(value) }

// ErrUnknown is wrapped by the error [Set.Parse] returns for a value that
// names no accepted format.
var ErrUnknown = errors.New("unknown output format")

// Parse reads value as one of the formats in s. Matching ignores case and
// surrounding space, and accepts the aliases yml, md, table and txt for a
// format s holds. The error for any other value names every accepted format.
func (s Set) Parse(value string) (Format, error) {
	name := strings.ToLower(strings.TrimSpace(value))
	for _, f := range s {
		if strings.ToLower(string(f)) == name {
			return f, nil
		}
	}
	if f, ok := aliases[name]; ok && s.Contains(f) {
		return f, nil
	}
	return "", fmt.Errorf("%w %q: want one of %s", ErrUnknown, value, s)
}

// Contains reports whether s holds f.
func (s Set) Contains(f Format) bool { return slices.Contains(s, f) }

// Names returns the flag spellings of the formats in s, for shell completion.
func (s Set) Names() []string {
	names := make([]string, len(s))
	for i, f := range s {
		names[i] = string(f)
	}
	return names
}

// String returns the formats in s as a comma-separated list for help text.
func (s Set) String() string { return strings.Join(s.Names(), ", ") }

// Value is a flag value that accepts the formats of one [Set]. It implements
// the Set, String and Type methods of pflag.Value and the Set and String
// methods of flag.Value, so it works with either package.
type Value struct {
	target  *Format
	allowed Set
}

// NewValue returns a flag value that stores into target, starting at def,
// and accepts the formats in allowed (or [Default] when allowed is nil).
func NewValue(target *Format, def Format, allowed Set) *Value {
	if allowed == nil {
		allowed = Default
	}
	*target = def
	return &Value{target: target, allowed: allowed}
}

// Set parses value and stores it.
func (v *Value) Set(value string) error {
	f, err := v.allowed.Parse(value)
	if err != nil {
		return err
	}
	*v.target = f
	return nil
}

// String returns the stored format.
func (v *Value) String() string {
	if v == nil || v.target == nil {
		return ""
	}
	return string(*v.target)
}

// Type names the value kind for pflag's help output.
func (v *Value) Type() string { return "format" }

// Allowed returns the formats the value accepts, for shell completion.
func (v *Value) Allowed() Set { return v.allowed }
