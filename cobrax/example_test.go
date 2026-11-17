package cobrax_test

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mittwald/cli-kit/buildinfo"
	"github.com/mittwald/cli-kit/cobrax"
	"github.com/mittwald/cli-kit/term"
)

func ExampleAddEnumFlag() {
	root := &cobra.Command{
		Use: "example",
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, _ := cmd.Flags().GetString("output")
			fmt.Println("format:", format)
			return nil
		},
	}
	cobrax.AddEnumFlag(root, root.Flags(), "output", "o", "text", "output format", "text", "json", "yaml")
	root.SetArgs([]string{"--output", "YAML"})
	_ = root.Execute()
	// Output:
	// format: yaml
}

func ExampleJoinSpacedBoolValues() {
	root := &cobra.Command{Use: "example", Run: func(*cobra.Command, []string) {}}
	root.Flags().Bool("stable", true, "mark the release stable")
	cobrax.AllowSpacedBool(root.Flags(), "stable")

	fmt.Println(cobrax.JoinSpacedBoolValues(root, []string{"--stable", "false", "release-1"}))
	// Output:
	// [--stable=false release-1]
}

func ExampleSetVersion() {
	root := &cobra.Command{Use: "example", Run: func(*cobra.Command, []string) {}}
	cobrax.SetVersion(root, buildinfo.Info{Version: "v1.4.0"})
	root.SetArgs([]string{"--version"})
	_ = root.Execute()
	// Output:
	// example 1.4.0
}

// A --color flag read the way grep reads it: a bare --color means auto, and
// only auto, always and never are accepted.
func ExampleAddColorFlagWith() {
	defer term.SetColorMode(term.CurrentColorMode())
	root := &cobra.Command{Use: "example", SilenceErrors: true, SilenceUsage: true, Run: func(*cobra.Command, []string) {}}
	color := cobrax.AddColorFlagWith(root, "when to use color: auto, always or never",
		cobrax.BareColor(term.ColorAuto), cobrax.StrictColor())

	root.SetArgs([]string{"--color"})
	_ = root.Execute()
	fmt.Println(color.Mode())

	root.SetArgs([]string{"--color=yes"})
	fmt.Println(root.Execute())
	// Output:
	// auto
	// invalid argument "yes" for "--color" flag: invalid color mode "yes": want one of auto, always, never
}

// Filter keys complete without a space after them, so the value can follow.
func ExampleCompletePrefix() {
	keys := []cobra.Completion{"name=\tfilter by name", "namespace=", "limit"}
	matches, directive := cobrax.CompletePrefix(keys, "na")
	fmt.Printf("%q %v\n", matches, directive&cobra.ShellCompDirectiveNoSpace != 0)
	// Output:
	// ["name=\tfilter by name" "namespace="] true
}

// An older spelling keeps working, says what to use instead, and is not
// shown in help.
func ExampleAddFlagAlias() {
	root := &cobra.Command{Use: "example", Run: func(*cobra.Command, []string) {}}
	links := cobrax.AddModeFlag(root, "links", "when to make links: auto, always or never")
	cobrax.AddFlagAlias(root, root.PersistentFlags(), cobrax.FlagAlias{Old: "no-links", New: "links", Value: "never"})
	root.SetErr(os.Stdout)
	root.SetArgs([]string{"--no-links"})
	_ = root.Execute()
	fmt.Println(links.Mode())
	// Output:
	// WARNING: --no-links is deprecated, use --links=never
	// never
}
