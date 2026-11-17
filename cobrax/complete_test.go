package cobrax

import (
	"slices"
	"testing"

	"github.com/spf13/cobra"
)

func TestSafeCompletions(t *testing.T) {
	tests := []struct {
		name      string
		candidate string
		safe      bool
	}{
		{"plain", "plain", true},
		{"described", "with\tdescription", true},
		{"described with spaces", "name\ta longer description", true},
		{"unicode", "größe\tÜbersicht", true},
		{"empty description", "value\t", true},
		{"empty", "", false},
		{"description only", "\tno value", false},
		{"escape in value", "clip\x1b]52;c;ZXZpbA==\x07board", false},
		{"escape in description", "described\twith \x1b[31mcolor", false},
		{"newline", "two\nlines", false},
		{"carriage return", "over\rwritten", false},
		{"second tab", "a\tb\tc", false},
		{"C1 control", "c1\u009b2Jcontrol", false},
		{"DEL", "del\x7f", false},
		{"invalid UTF-8", "stray\x9b2J", false},
		{"invalid UTF-8 in description", "ok\tstray\xff", false},
	}
	var values, want []cobra.Completion
	for _, tt := range tests {
		values = append(values, tt.candidate)
		if tt.safe {
			want = append(want, tt.candidate)
		}
		if got := SafeCompletions([]cobra.Completion{tt.candidate}); (len(got) == 1) != tt.safe {
			t.Errorf("%s: SafeCompletions(%q) = %q, want safe = %v", tt.name, tt.candidate, got, tt.safe)
		}
	}
	original := slices.Clone(values)
	if got := SafeCompletions(values); !slices.Equal(got, want) {
		t.Errorf("SafeCompletions = %q, want %q", got, want)
	}
	if !slices.Equal(values, original) {
		t.Error("SafeCompletions changed its argument")
	}
	if got := SafeCompletions(nil); got == nil || len(got) != 0 {
		t.Errorf("SafeCompletions(nil) = %#v, want an empty slice", got)
	}
}

func TestCompletePrefix(t *testing.T) {
	keys := []cobra.Completion{"name=\tfilter by name", "namespace=", "status=", "limit"}
	tests := []struct {
		name       string
		candidates []cobra.Completion
		typed      string
		want       []cobra.Completion
		directive  cobra.ShellCompDirective
	}{
		{"all keys", keys, "", keys, cobra.ShellCompDirectiveNoFileComp},
		{"only keys that want a value", keys, "na", keys[:2], cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace},
		{"one key", keys, "st", keys[2:3], cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace},
		{"a finished word", keys, "li", keys[3:], cobra.ShellCompDirectiveNoFileComp},
		{"the description is not matched", keys, "filter", nil, cobra.ShellCompDirectiveNoFileComp},
		{"nothing", nil, "", nil, cobra.ShellCompDirectiveNoFileComp},
		{"unsafe candidates go", []cobra.Completion{"bad\x1b[2J=", "good="}, "", []cobra.Completion{"good="}, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace},
		{"case matters", []cobra.Completion{"Name"}, "na", nil, cobra.ShellCompDirectiveNoFileComp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, directive := CompletePrefix(tt.candidates, tt.typed)
			if !slices.Equal(got, tt.want) || directive != tt.directive {
				t.Errorf("CompletePrefix(%q) = %q, %v; want %q, %v", tt.typed, got, directive, tt.want, tt.directive)
			}
		})
	}
}

// A completion function built on CompletePrefix reaches the shell with its
// directive.
func TestCompletePrefixCommand(t *testing.T) {
	root := newRoot()
	list, _, _ := root.Find([]string{"list"})
	mustRegister(list, "name", func(_ *cobra.Command, _ []string, typed string) ([]cobra.Completion, cobra.ShellCompDirective) {
		return CompletePrefix([]cobra.Completion{"alice=", "bob="}, typed)
	})
	candidates, directive := complete(t, root, "list", "--name", "a")
	if !slices.Equal(candidates, []string{"alice="}) || directive != ":6" {
		t.Errorf("completion = %q %s", candidates, directive)
	}
}
