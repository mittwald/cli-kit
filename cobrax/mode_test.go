package cobrax

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/mittwald/cli-kit/term"
)

func TestModeFlag(t *testing.T) {
	keepColorMode(t)
	strict := []ModeOption{ModeAliases("hyperlinks"), BareMode(term.ColorAuto), StrictMode(),
		ModeError(func(value string) error { return fmt.Errorf("Invalid --links %q", value) })}
	tests := []struct {
		opts []ModeOption
		args []string
		want term.ColorMode
		err  string
	}{
		{nil, []string{"show"}, term.ColorAuto, ""},
		{nil, []string{"--links", "show"}, term.ColorAlways, ""},
		{nil, []string{"--links=never", "show"}, term.ColorNever, ""},
		{nil, []string{"show", "--links=NEVER"}, term.ColorNever, ""},
		{nil, []string{"--links=yes", "show"}, term.ColorAlways, ""},
		{nil, []string{"--links=tty", "show"}, term.ColorAuto, ""},
		{nil, []string{"--links=sometimes", "show"}, term.ColorAuto,
			`invalid argument "sometimes" for "--links" flag: invalid value "sometimes": want one of auto, always, never`},
		{[]ModeOption{StrictMode()}, []string{"--links=none", "show"}, term.ColorAuto,
			`invalid argument "none" for "--links" flag: invalid value "none": want one of auto, always, never`},
		{[]ModeOption{BareMode(term.ColorNever)}, []string{"--links=true", "show"}, term.ColorNever, ""},
		{strict, []string{"--links", "show"}, term.ColorAuto, ""},
		{strict, []string{"--hyperlinks=always", "show"}, term.ColorAlways, ""},
		{strict, []string{"--links=Never", "show"}, term.ColorNever, ""},
		{strict, []string{"--links=yes", "show"}, term.ColorAuto, `invalid argument "yes" for "--links" flag: Invalid --links "yes"`},
	}
	for _, tt := range tests {
		term.SetColorMode(term.ColorNever)
		root := newRoot()
		value := AddModeFlag(root, "links", "when to make links", tt.opts...)
		_, err := execute(t, root, tt.args...)
		switch {
		case tt.err == "" && err != nil:
			t.Errorf("%q: %v", tt.args, err)
		case tt.err != "" && (err == nil || err.Error() != tt.err):
			t.Errorf("%q: error = %v, want %s", tt.args, err, tt.err)
		case value.Mode() != tt.want:
			t.Errorf("%q: mode = %v, want %v", tt.args, value.Mode(), tt.want)
		}
		// A mode flag is no color flag: the process's color mode stays.
		if term.CurrentColorMode() != term.ColorNever {
			t.Errorf("%q: the color mode changed to %v", tt.args, term.CurrentColorMode())
		}
	}

	root := newRoot()
	value := AddModeFlag(root, "links", "when to make links", strict...)
	for _, name := range []string{"links", "hyperlinks"} {
		flag := root.PersistentFlags().Lookup(name)
		if flag.NoOptDefVal != "auto" || flag.Value.Type() != "when" || flag.Value.String() != "auto" {
			t.Errorf("--%s: bare %q, type %q, value %q", name, flag.NoOptDefVal, flag.Value.Type(), flag.Value.String())
		}
	}
	if !value.IsBoolFlag() {
		t.Error("a bare flag is not accepted by the flag package")
	}
	candidates, directive := complete(t, root, "--links=")
	if !slices.Equal(candidates, term.ColorModeNames) || directive != noFileDirective {
		t.Errorf("--links completion = %q %s", candidates, directive)
	}
}

func TestFlagAlias(t *testing.T) {
	newAliased := func() (*cobra.Command, *ModeValue, *string, *bytes.Buffer) {
		root := newRoot()
		links := AddModeFlag(root, "links", "when to make links")
		env := root.PersistentFlags().String("env", "production", "environment")
		AddFlagAlias(root, root.PersistentFlags(), FlagAlias{Old: "no-links", New: "links", Value: "never"})
		AddFlagAlias(root, root.PersistentFlags(), FlagAlias{Old: "staging", New: "env", Value: "staging", Silent: true})
		var stderr bytes.Buffer
		root.SetErr(&stderr)
		return root, links, env, &stderr
	}
	const note = "WARNING: --no-links is deprecated, use --links=never\n"
	tests := []struct {
		args   []string
		links  term.ColorMode
		env    string
		stderr string
		err    string
	}{
		{[]string{"show"}, term.ColorAuto, "production", "", ""},
		{[]string{"--no-links", "show"}, term.ColorNever, "production", note, ""},
		{[]string{"--no-links", "show", "--no-links"}, term.ColorNever, "production", note, ""},
		{[]string{"show", "--no-links=false"}, term.ColorAuto, "production", note, ""},
		{[]string{"show", "--staging"}, term.ColorAuto, "staging", "", ""},
		{[]string{"--staging", "--env=dev", "show"}, term.ColorAuto, "dev", "", ""},
		{[]string{"--no-links=maybe", "show"}, term.ColorAuto, "production", "",
			`invalid argument "maybe" for "--no-links" flag: invalid value "maybe": want true or false`},
	}
	for _, tt := range tests {
		root, links, env, stderr := newAliased()
		root.SetArgs(tt.args)
		var stdout bytes.Buffer
		root.SetOut(&stdout)
		err := root.Execute()
		switch {
		case tt.err == "" && err != nil:
			t.Errorf("%q: %v", tt.args, err)
		case tt.err != "" && (err == nil || err.Error() != tt.err):
			t.Errorf("%q: error = %v, want %s", tt.args, err, tt.err)
		case links.Mode() != tt.links || *env != tt.env || stderr.String() != tt.stderr:
			t.Errorf("%q: links %v, env %q, stderr %q; want %v, %q, %q", tt.args, links.Mode(), *env, stderr.String(), tt.links, tt.env, tt.stderr)
		}
	}

	// The target counts as given.
	root, _, _, _ := newAliased()
	show, _, _ := root.Find([]string{"show"})
	changed := false
	show.RunE = func(cmd *cobra.Command, _ []string) error {
		changed = cmd.Flags().Changed("links")
		return nil
	}
	if _, err := execute(t, root, "show", "--no-links"); err != nil || !changed {
		t.Errorf("--no-links: err %v, --links changed = %v", err, changed)
	}

	// The aliases are hidden from help and completion.
	root, _, _, _ = newAliased()
	help, err := execute(t, root, "show", "--help")
	if err != nil || strings.Contains(help, "no-links") || strings.Contains(help, "staging") {
		t.Errorf("help shows the aliases (err %v):\n%s", err, help)
	}
	root, _, _, _ = newAliased()
	if candidates, _ := complete(t, root, "show", "--"); slices.Contains(candidates, "--no-links") || slices.Contains(candidates, "--staging") {
		t.Errorf("completion offers the aliases: %q", candidates)
	}
}

func TestFlagAliasPanics(t *testing.T) {
	for name, alias := range map[string]FlagAlias{
		"missing target": {Old: "old", New: "missing", Value: "x"},
		"empty name":     {Old: "", New: "links", Value: "x"},
		"taken name":     {Old: "links", New: "links", Value: "x"},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("no panic")
				}
			}()
			root := newRoot()
			AddModeFlag(root, "links", "when to make links")
			AddFlagAlias(root, root.PersistentFlags(), alias)
		})
	}
}
