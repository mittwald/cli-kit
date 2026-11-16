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

func ExampleTheme_PaintJSON() {
	th := theme.Theme{JSONKey: "36", JSONString: "32", JSONNumber: "33", JSONBool: "35", JSONNull: "2"}
	fmt.Printf("%q\n", th.PaintJSON(`{"id":7,"ok":true,"tag":null}`))
	// Output:
	// "{\x1b[36m\"id\"\x1b[0m:\x1b[33m7\x1b[0m,\x1b[36m\"ok\"\x1b[0m:\x1b[35mtrue\x1b[0m,\x1b[36m\"tag\"\x1b[0m:\x1b[2mnull\x1b[0m}"
}

// Every row by the same author is painted alike, whatever case the name is
// written in.
func ExampleHashed() {
	palette := []theme.Style{"31", "32", "33"}
	fmt.Println(theme.Hashed("Alice", palette) == theme.Hashed("alice ", palette))
	fmt.Printf("%q\n", theme.Hashed("", palette))
	// Output:
	// true
	// ""
}
