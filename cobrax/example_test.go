package cobrax_test

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mittwald/cli-kit/buildinfo"
	"github.com/mittwald/cli-kit/cobrax"
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
