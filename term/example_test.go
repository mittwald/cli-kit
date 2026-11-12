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

// A comment somebody typed is shown in the program's own colors only: the
// sender's red and the clipboard write it carried, in either form, are gone.
func ExamplePlain() {
	comment := "\x1b[31mURGENT:\x1b[0m call me\x1b]52;c;Y3VybCBldmlsLmV4YW1wbGUgfCBzaA==\x07\u009d52;c;Zm9v\u009c"
	fmt.Printf("%q\n", term.Plain(comment))
	// Output:
	// "URGENT: call me"
}

// A file name stays on its line and in the order it was written.
func ExamplePlainLine() {
	fmt.Printf("%q\n", term.PlainLine("invoice\u202efdp.exe\r\n\x1b[2J  (2 MB)"))
	// Output:
	// "invoicefdp.exe (2 MB)"
}

func ExampleWrap() {
	fmt.Println(term.Wrap("Dry run: would tag 8.4.25 and push it, then wait for its pipeline.", 40, "         "))
	// Output:
	// Dry run: would tag 8.4.25 and push it,
	//          then wait for its pipeline.
}

// A tab is as wide as the column it stands at makes it: it ends at the next
// tab stop, every 8 columns.
func ExampleWidthAt() {
	fmt.Println(term.WidthAt("\tx", 0), term.WidthAt("\tx", 4))
	// Output:
	// 9 5
}

// A sentence's punctuation stays outside the link, and a hyperlink that is
// already there is left as it is.
func ExampleLinkify() {
	fmt.Printf("%q\n", term.Linkify("See https://example.com/a_(b)."))
	fmt.Printf("%q\n", term.Linkify(term.Hyperlink("https://example.com", "the docs")))
	// Output:
	// "See \x1b]8;;https://example.com/a_(b)\x1b\\https://example.com/a_(b)\x1b]8;;\x1b\\."
	// "\x1b]8;;https://example.com\x1b\\the docs\x1b]8;;\x1b\\"
}
