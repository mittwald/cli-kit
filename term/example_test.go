package term_test

import (
	"fmt"

	"github.com/mittwald/cli-kit/term"
)

// A job log keeps its colors; the title it tried to set, the line it erased
// and the progress it redrew in place are gone.
func ExampleSanitize() {
	log := "\x1b]0;pwned\x07\x1b[32;1m$ make test\x1b[0;m\n" +
		"downloading 10%\rdownloading 100%\x1b[2K\n" +
		"\x1b[31mFAIL\x1b[0m\tpkg/foo\n"
	fmt.Printf("%q\n", term.Sanitize(log))
	// Output:
	// "\x1b[32;1m$ make test\x1b[0;m\ndownloading 100%\n\x1b[31mFAIL\x1b[0m    pkg/foo\n"
}

// The last line of a log, shown alone, keeps the color an earlier line set.
func ExampleSanitizeLines() {
	lines := term.SanitizeLines("\x1b[31mERROR: build failed:\nexit code 1\x1b[0m\n")
	fmt.Printf("%q\n", lines[len(lines)-1])
	// Output:
	// "\x1b[31mexit code 1\x1b[0m"
}

func ExampleWrap() {
	fmt.Println(term.Wrap("Dry run: would tag 8.4.25 and push it, then wait for its pipeline.", 40, "         "))
	// Output:
	// Dry run: would tag 8.4.25 and push it,
	//          then wait for its pipeline.
}
