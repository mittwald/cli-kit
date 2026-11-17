package theme

import (
	"testing"

	"github.com/mittwald/cli-kit/term"
)

// A style a user wrote, whatever it says, only ever colors text: painting
// with it adds escape sequences and nothing else, so taking them out gives
// the text back.
func FuzzParseStyle(f *testing.F) {
	for _, seed := range []string{"245", "160:bold", "fg=white:bg=#aa0000:underline", "0x1f2", "bg=300", "::", "red:;31m"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, spec string) {
		style, ok := ParseStyle(spec)
		if !ok {
			return
		}
		const text = "text"
		painted := style.Paint(text)
		if got := term.StripANSI(painted); got != text {
			t.Fatalf("ParseStyle(%q) = %q paints %q, which shows as %q", spec, style, painted, got)
		}
		if term.Sanitize(painted) == "" {
			t.Fatalf("ParseStyle(%q) = %q paints nothing visible", spec, style)
		}
	})
}
