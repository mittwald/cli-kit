package cobrax

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/mittwald/cli-kit/buildinfo"
)

// SetVersion sets root.Version from info, which turns on cobra's --version
// flag, and makes it print "name version" on one line.
func SetVersion(root *cobra.Command, info buildinfo.Info) {
	root.Version = info.String()
	root.SetVersionTemplate("{{.Name}} {{.Version}}\n")
}

// NewVersionCommand returns a "version" command that prints info in detail:
// the version, the revision, the build date and the Go release.
func NewVersionCommand(info buildinfo.Info) *cobra.Command {
	return &cobra.Command{
		Use:               "version",
		Short:             "Print version information",
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := io.WriteString(cmd.OutOrStdout(), info.Detail(cmd.Root().Name()))
			return err
		},
	}
}
