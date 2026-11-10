package term

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
)

// ColorMode is what the user asked for about color, usually through a
// --color flag.
type ColorMode int

const (
	// ColorAuto paints output written to a terminal and leaves everything
	// else plain. It honors NO_COLOR and TERM=dumb. It is the default.
	ColorAuto ColorMode = iota
	// ColorAlways paints output wherever it goes, even into a pipe.
	ColorAlways
	// ColorNever never paints output.
	ColorNever
)

// ColorModeNames are the spellings [ParseColorMode] accepts, in the order a
// help text or shell completion lists them.
var ColorModeNames = []string{"auto", "always", "never"}

// String returns the flag spelling of m.
func (m ColorMode) String() string {
	switch m {
	case ColorAlways:
		return "always"
	case ColorNever:
		return "never"
	default:
		return "auto"
	}
}

// ParseColorMode reads a --color value. Besides auto, always and never it
// takes the spellings other tools use for the same three choices: an empty
// value or "true" means always (a bare --color), "false" and "none" mean
// never, and "tty" and "if-tty" mean auto.
func ParseColorMode(value string) (ColorMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto", "tty", "if-tty":
		return ColorAuto, nil
	case "always", "", "true", "yes", "force":
		return ColorAlways, nil
	case "never", "false", "no", "none":
		return ColorNever, nil
	}
	return ColorAuto, fmt.Errorf("invalid color mode %q: want one of %s", value, strings.Join(ColorModeNames, ", "))
}

var colorMode atomic.Int32

// SetColorMode records the color choice for the whole process. Call it once,
// after parsing flags and before writing output. A value other than the
// three defined modes records [ColorAuto].
func SetColorMode(m ColorMode) {
	if m < ColorAuto || m > ColorNever {
		m = ColorAuto
	}
	colorMode.Store(int32(m))
}

// CurrentColorMode returns the mode [SetColorMode] last recorded.
func CurrentColorMode() ColorMode { return ColorMode(colorMode.Load()) }

// ColorAllowed reports whether the environment permits color: NO_COLOR is
// unset (any value, even empty, turns color off, see https://no-color.org)
// and TERM is not "dumb".
func ColorAllowed() bool {
	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}
	return os.Getenv("TERM") != "dumb"
}

// ShouldColor reports whether output written to w is painted under mode m:
// always under [ColorAlways], never under [ColorNever], and under
// [ColorAuto] only when w is a terminal and [ColorAllowed] holds.
func ShouldColor(m ColorMode, w io.Writer) bool {
	switch m {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	}
	return ColorAllowed() && IsTerminal(w)
}

// Colored reports whether output written to w is painted under the mode
// [SetColorMode] recorded.
func Colored(w io.Writer) bool { return ShouldColor(CurrentColorMode(), w) }

// Interactive drawing — redrawing a block in place, hiding the cursor — needs
// a real terminal whatever the color mode says: an escape sequence that
// moves the cursor ruins a log file even under --color=always.

// CanAnimate reports whether w may be redrawn in place: it is a terminal, the
// environment permits color, and the user did not ask for --color=never.
func CanAnimate(w io.Writer) bool {
	return CurrentColorMode() != ColorNever && ColorAllowed() && IsTerminal(w)
}
