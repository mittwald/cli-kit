package format

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/mittwald/cli-kit/term"
)

func TestParseAcceptsCaseSpaceAndAliases(t *testing.T) {
	for value, want := range map[string]Format{
		"json":     JSON,
		" JSON ":   JSON,
		"Yaml":     YAML,
		"yml":      YAML,
		"md":       Markdown,
		"markdown": Markdown,
		"table":    Text,
		"txt":      Text,
		"text":     Text,
		"csv":      CSV,
		"TSV":      TSV,
	} {
		got, err := Parse(value)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %q, %v; want %q", value, got, err, want)
		}
	}
}

func TestParseRejectsUnknownAndNamesAccepted(t *testing.T) {
	_, err := Parse("xml")
	if !errors.Is(err, ErrUnknown) {
		t.Fatalf("Parse(xml) error = %v, want ErrUnknown", err)
	}
	for _, name := range []string{`"xml"`, "text, json, yaml, csv, tsv, markdown"} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("error %q does not mention %s", err, name)
		}
	}
}

// A restricted set rejects formats it does not hold, aliases included, and
// accepts formats of the caller's own.
func TestRestrictedSet(t *testing.T) {
	ics := Format("ics")
	set := Set{Text, JSON, ics}
	if _, err := set.Parse("yaml"); err == nil {
		t.Fatal("restricted set accepted yaml")
	}
	if _, err := set.Parse("yml"); err == nil {
		t.Fatal("restricted set accepted the yml alias")
	}
	if got, err := set.Parse("ICS"); err != nil || got != ics {
		t.Fatalf("Parse(ICS) = %q, %v", got, err)
	}
	if got := set.String(); got != "text, json, ics" {
		t.Fatalf("String() = %q", got)
	}
	if !set.Contains(ics) || set.Contains(CSV) {
		t.Fatal("Contains misreports membership")
	}
}

func TestClassification(t *testing.T) {
	cases := []struct {
		f                               Format
		structured, delimited, verbatim bool
	}{
		{Text, false, false, false},
		{JSON, true, false, false},
		{YAML, true, false, false},
		{CSV, false, true, true},
		{TSV, false, true, true},
		{Markdown, false, false, true},
		{Format("ics"), false, false, false},
	}
	for _, c := range cases {
		if c.f.IsStructured() != c.structured || c.f.IsDelimited() != c.delimited || c.f.IsVerbatim() != c.verbatim {
			t.Errorf("%s: structured=%v delimited=%v verbatim=%v", c.f, c.f.IsStructured(), c.f.IsDelimited(), c.f.IsVerbatim())
		}
	}
}

// Only text is ever painted: verbatim and structured output stays plain even
// under --color=always.
func TestOnlyTextIsColored(t *testing.T) {
	prev := term.CurrentColorMode()
	t.Cleanup(func() { term.SetColorMode(prev) })
	term.SetColorMode(term.ColorAlways)
	var buf bytes.Buffer
	for _, f := range Default {
		if got, want := f.Colored(&buf), f == Text; got != want {
			t.Errorf("%s.Colored under always = %v, want %v", f, got, want)
		}
	}
	term.SetColorMode(term.ColorNever)
	if Text.Colored(&buf) {
		t.Error("text colored under never")
	}
}

func TestValueWorksAsFlag(t *testing.T) {
	var out Format
	value := NewValue(&out, Text, Set{Text, JSON})
	if out != Text || value.String() != "text" {
		t.Fatalf("default not applied: %q", out)
	}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(&bytes.Buffer{})
	fs.Var(value, "output", "output format")
	if err := fs.Parse([]string{"-output", "JSON"}); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if out != JSON {
		t.Fatalf("out = %q, want json", out)
	}
	if err := fs.Parse([]string{"-output", "csv"}); err == nil {
		t.Fatal("flag accepted a format outside its set")
	}
	if value.Type() != "format" || len(value.Allowed()) != 2 {
		t.Fatalf("Type=%q Allowed=%v", value.Type(), value.Allowed())
	}
}

func TestNilAllowedMeansDefault(t *testing.T) {
	var out Format
	value := NewValue(&out, JSON, nil)
	if err := value.Set("markdown"); err != nil || out != Markdown {
		t.Fatalf("Set(markdown) = %v, out %q", err, out)
	}
}
