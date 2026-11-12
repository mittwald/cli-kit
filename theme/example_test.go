package theme_test

import (
	"fmt"
	"strings"

	"github.com/mittwald/cli-kit/theme"
)

var diff = "--- a/version.yaml\n" +
	"+++ b/version.yaml\n" +
	"@@ -1,2 +1,2 @@\n" +
	"-version: 8.4.24\n" +
	"+version: 8.4.25\n" +
	" stable: true\n"

func ExampleTheme_PaintDiff() {
	th := theme.Theme{Added: "32", Removed: "31", DiffHeader: theme.Bold, DiffHunk: "36"}
	for line := range strings.Lines(th.PaintDiff(diff)) {
		fmt.Printf("%q\n", line)
	}
	// Output:
	// "\x1b[1m--- a/version.yaml\x1b[0m\n"
	// "\x1b[1m+++ b/version.yaml\x1b[0m\n"
	// "\x1b[36m@@ -1,2 +1,2 @@\x1b[0m\n"
	// "\x1b[31m-version: 8.4.24\x1b[0m\n"
	// "\x1b[32m+version: 8.4.25\x1b[0m\n"
	// " stable: true\n"
}

// A review shows only what a change removes and adds, painted where the
// output takes color: here Plain stands in for theme.Active().For(os.Stdout)
// on a pipe.
func ExampleDiffScanner() {
	th := theme.Plain
	var scanner theme.DiffScanner
	for line := range strings.SplitSeq(strings.TrimSuffix(diff, "\n"), "\n") {
		switch kind := scanner.Classify(line); kind {
		case theme.DiffRemoved, theme.DiffAdded:
			fmt.Println("    " + th.PaintDiffLine(kind, line))
		}
	}
	// Output:
	//     -version: 8.4.24
	//     +version: 8.4.25
}
