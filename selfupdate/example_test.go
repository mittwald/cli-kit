package selfupdate_test

import (
	"context"
	"fmt"
	"os"

	"github.com/mittwald/cli-kit/selfupdate"
)

// A tool released with GoReleaser to a GitLab project's Generic Package
// Registry updates itself with a personal access token from $EXAMPLE_TOKEN.
func ExampleUpdater() {
	u := &selfupdate.Updater{
		Source: &selfupdate.GitLab{
			BaseURL: "https://gitlab.example.com",
			Project: "example/tool",
			Package: "tool",
			Token: func(context.Context, bool) (string, error) {
				return os.Getenv("EXAMPLE_TOKEN"), nil
			},
		},
		Current: "v1.0.0", // usually injected with -ldflags
		Asset:   selfupdate.TitleCaseAssets("tool"),
	}
	result, err := u.Update(context.Background(), false)
	switch {
	case err != nil:
		fmt.Fprintln(os.Stderr, err)
	case result.Updated:
		fmt.Printf("updated %s from %s to %s\n", result.Path, result.From, result.Release.Tag)
	default:
		fmt.Printf("already up to date (%s)\n", result.From)
	}
}

func ExampleVersionedAssets() {
	name := selfupdate.VersionedAssets("tool")(selfupdate.Release{Tag: "v1.2.3"},
		selfupdate.Platform{OS: "linux", Arch: "arm", Arm: "7"})
	fmt.Println(name)
	// Output: tool-1.2.3-linux-armv7.tar.gz
}

func ExampleCompare() {
	fmt.Println(selfupdate.Compare("v1.10.0", "v1.9.0"), selfupdate.Compare("1.0.0-rc.1", "1.0.0"))
	// Output: 1 -1
}
