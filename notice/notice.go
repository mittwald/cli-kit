package notice

import (
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mittwald/cli-kit/term"
)

// Background is the background of the terminal a notice goes to, which
// picks between a theme's Dark and Light colors.
type Background int

// The backgrounds.
const (
	// BackgroundAuto reads COLORFGBG, which some terminals set, and takes
	// the dark colors when it does not tell.
	BackgroundAuto Background = iota
	// BackgroundDark takes the dark colors.
	BackgroundDark
	// BackgroundLight takes the light colors.
	BackgroundLight
)

// Options configure a [Printer]. The zero Options print the [Line] theme to
// os.Stderr.
type Options struct {
	// Theme is how notices look. The zero Theme is [Line]; a Theme without
	// dark colors of its own takes [Default].
	Theme Theme
	// Background picks the theme's dark or light colors.
	Background Background
	// Colors are set over the theme's colors, one color at a time: a color
	// they leave empty keeps the theme's. As in a theme, a color Light
	// leaves empty is taken from Dark.
	Colors Colors
	// Writer is where notices go; os.Stderr when nil. Whether they are
	// painted is decided for it by term.Colored.
	Writer io.Writer
	// Debug turns on the debug notices, usually with a --verbose flag.
	Debug bool
	// Around, when set, writes each notice: it gets a function that writes
	// it and calls that function, so a progress bar or a live display can
	// step aside first, as progress.Bar.WithCleared does.
	Around func(write func())
	// KeepEscapes leaves escape sequences and control characters in the
	// text of a message and its arguments instead of making it plain with
	// term.Plain.
	//
	// Use it only for text the program wrote itself. Text from a server, a
	// file or an error can then move the cursor, rewrite earlier lines or
	// change the terminal's title.
	KeepEscapes bool
	// NoRedact leaves recognizable credentials, such as password=… or a
	// Bearer token, in the text instead of replacing them with
	// redact.Patterns.
	//
	// Use it only for text that cannot carry a credential; otherwise a
	// notice can put a secret on the screen and into a terminal's
	// scrollback or a CI log.
	NoRedact bool
}

// Printer writes notices. It is safe for concurrent use; notices from
// several goroutines never interleave.
type Printer struct {
	options Options
	mu      sync.Mutex
}

// New returns a Printer with options.
func New(options Options) *Printer {
	if options.Writer == nil {
		options.Writer = os.Stderr
	}
	switch t := options.Theme; {
	case t.Name == "" && t.Layout == LayoutLine && t.Colors.Dark.Levels == nil && t.Colors.Light.Levels == nil &&
		t.Padding == (Padding{}) && !t.NoFill && len(t.Patterns) == 0:
		options.Theme = Line // the zero Theme
	case t.Colors.Dark.Levels == nil:
		options.Theme.Colors = Default // a theme that sets no colors of its own
	}
	return &Printer{options: options}
}

var active atomic.Pointer[Printer]

// Use installs p as the Printer of the package-level functions, [Errorf]
// and the rest, for the whole process. Call it once at start-up.
func Use(p *Printer) { active.Store(p) }

// Active returns the Printer [Use] installed, or one with the zero
// [Options].
func Active() *Printer {
	if p := active.Load(); p != nil {
		return p
	}
	p := New(Options{})
	if active.CompareAndSwap(nil, p) {
		return p
	}
	return active.Load()
}

// Errorf writes an error notice. Arguments are formatted as fmt.Sprintf
// formats them; mark the values the message carries with [Value] or its
// shorthands, such as [Date] and [Path], to color them.
func (p *Printer) Errorf(format string, args ...any) { p.Notify(LevelError, format, args...) }

// Warnf writes a warning notice; see [Printer.Errorf].
func (p *Printer) Warnf(format string, args ...any) { p.Notify(LevelWarn, format, args...) }

// Infof writes an informational notice; see [Printer.Errorf].
func (p *Printer) Infof(format string, args ...any) { p.Notify(LevelInfo, format, args...) }

// Successf writes an OK notice; see [Printer.Errorf].
func (p *Printer) Successf(format string, args ...any) { p.Notify(LevelOK, format, args...) }

// Debugf writes a debug notice when [Options] Debug is set; see
// [Printer.Errorf].
func (p *Printer) Debugf(format string, args ...any) { p.Notify(LevelDebug, format, args...) }

// Error writes err as an error notice. A nil err writes nothing.
func (p *Printer) Error(err error) {
	if err != nil {
		p.Notify(LevelError, "%v", err)
	}
}

// Warn writes err as a warning notice. A nil err writes nothing.
func (p *Printer) Warn(err error) {
	if err != nil {
		p.Notify(LevelWarn, "%v", err)
	}
}

// Notify writes a notice of level; see [Printer.Errorf].
func (p *Printer) Notify(level Level, format string, args ...any) {
	if level == LevelDebug && !p.options.Debug {
		return
	}
	colored := term.Colored(p.options.Writer)
	text := p.render(level, format, args, colored)
	write := func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		_, _ = io.WriteString(p.options.Writer, text)
	}
	if p.options.Around != nil {
		p.options.Around(write)
		return
	}
	write()
}

// Errorf writes an error notice with the [Active] Printer.
func Errorf(format string, args ...any) { Active().Errorf(format, args...) }

// Warnf writes a warning notice with the [Active] Printer.
func Warnf(format string, args ...any) { Active().Warnf(format, args...) }

// Infof writes an informational notice with the [Active] Printer.
func Infof(format string, args ...any) { Active().Infof(format, args...) }

// Successf writes an OK notice with the [Active] Printer.
func Successf(format string, args ...any) { Active().Successf(format, args...) }

// Debugf writes a debug notice with the [Active] Printer.
func Debugf(format string, args ...any) { Active().Debugf(format, args...) }

// Error writes err as an error notice with the [Active] Printer.
func Error(err error) { Active().Error(err) }

// Warn writes err as a warning notice with the [Active] Printer.
func Warn(err error) { Active().Warn(err) }

// lightBackground reports what COLORFGBG says of the background: its last
// field is the background's color number, and 7 and 15 (white) are the
// light ones, as the terminals that set it use them.
func lightBackground(colorfgbg string) bool {
	fields := strings.Split(colorfgbg, ";")
	n, err := strconv.Atoi(strings.TrimSpace(fields[len(fields)-1]))
	return err == nil && (n == 7 || n == 15)
}

// light reports whether the light colors apply.
func (p *Printer) light() bool {
	switch p.options.Background {
	case BackgroundLight:
		return true
	case BackgroundDark:
		return false
	}
	return lightBackground(os.Getenv("COLORFGBG"))
}
