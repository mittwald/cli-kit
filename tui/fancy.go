package tui

import (
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// Animated is a widget that changes without an event, such as a shimmer or
// a bar gliding to its value. While any widget on screen is Animating, the
// application redraws a few times a second.
type Animated interface {
	Widget
	Animating() bool
}

// MittwaldGradient runs from mittwald blue through purple and salmon to
// rose, the colors of mittwald's own command-line tools, lightened by a
// quarter as their progress lines are, so they read on a dark bar.
var MittwaldGradient = []theme.Style{
	theme.RGB(120, 106, 238),
	theme.RGB(180, 146, 226),
	theme.RGB(237, 184, 164),
	theme.RGB(200, 152, 164),
}

// trueColor reports whether the terminal takes 24-bit colors, asked once;
// a test may replace it.
var trueColor = sync.OnceValue(term.TrueColor)

// paint is the SGR for c as the foreground, or as the background with bg,
// in 24-bit: the canvas turns it into the nearest of the 256 colors where
// the terminal takes no 24-bit colors.
func paint(c rgb, bg bool) theme.Style {
	layer := "38"
	if bg {
		layer = "48"
	}
	return theme.Style(layer + ";2;" + strconv.Itoa(int(c.r)) + ";" + strconv.Itoa(int(c.g)) + ";" + strconv.Itoa(int(c.b)))
}

// stopColors are the colors of stops a palette knows, the others left out.
func stopColors(stops []theme.Style) []rgb {
	var out []rgb
	for _, s := range stops {
		if fg, _, _ := colors(s); fg.known {
			out = append(out, fg.c)
		}
	}
	return out
}

// sample is the color at t, from 0 to 1, of a gradient that runs through
// stops and back to the first, so it can flow without a seam. The stops
// blend with a smooth step.
func sample(stops []rgb, t float64) rgb {
	t -= math.Floor(t)
	pos := t * float64(len(stops))
	i := int(pos) % len(stops)
	f := pos - math.Floor(pos)
	f = f * f * (3 - 2*f)
	return mix(stops[i], stops[(i+1)%len(stops)], f)
}

// GradientText paints text in colors flowing through stops, one per
// character, shifted by phase: a phase from 0 to 1 moves the colors once
// through the text, so a phase that grows with time makes them flow. Stops
// that set no color of the 256-color palette or a true color are left out;
// with none left, the text is plain. Pass Bold or other attributes in
// style.
func GradientText(text string, stops []theme.Style, phase float64, style theme.Style) Line {
	cs := stopColors(stops)
	gs := graphemes(oneLine(text))
	if len(cs) == 0 || len(gs) == 0 {
		return Text(text, style)
	}
	out := make(Line, 0, len(gs))
	for i, g := range gs {
		c := sample(cs, float64(i)/float64(max(len(gs), 8))-phase)
		out = append(out, Span{Text: g.text, Style: theme.Join(style, paint(c, false))})
	}
	return out
}

// Shimmer is a line of text whose colors flow through a gradient while it
// is active, as the progress lines of mittwald's tools do, and stand still
// once it is not.
type Shimmer struct {
	text    string
	stops   []theme.Style
	style   theme.Style
	active  bool
	started time.Time
	// speed is how many times a second the colors pass through the text.
	speed float64
}

// NewShimmer returns an active shimmer of text in MittwaldGradient, bold.
func NewShimmer(text string) *Shimmer {
	return &Shimmer{text: text, stops: MittwaldGradient, style: theme.Bold, active: true, started: time.Now(), speed: 0.4}
}

// SetText sets the text.
func (s *Shimmer) SetText(text string) *Shimmer { s.text = text; return s }

// SetGradient sets the colors the text flows through.
func (s *Shimmer) SetGradient(stops ...theme.Style) *Shimmer { s.stops = stops; return s }

// SetStyle sets the attributes the text is drawn with, such as bold.
func (s *Shimmer) SetStyle(style theme.Style) *Shimmer { s.style = style; return s }

// SetActive starts or stops the flow.
func (s *Shimmer) SetActive(on bool) *Shimmer { s.active = on; return s }

// Animating reports whether the colors flow.
func (s *Shimmer) Animating() bool { return s.active }

// Line is the text as drawn now.
func (s *Shimmer) Line() Line {
	phase := 0.0
	if s.active {
		phase = time.Since(s.started).Seconds() * s.speed
	}
	return GradientText(s.text, s.stops, phase, s.style)
}

// Draw draws the text.
func (s *Shimmer) Draw(c Canvas) { c.Line(0, 0, s.Line()) }

// Size is the text on one line.
func (s *Shimmer) Size(width, _ int) (int, int) { return min(textWidth(s.text), width), 1 }

// ProgressBar is a bar that fills from the left with its label and the
// percentage inside it: dark text on a light fill, light text on a dark
// track of the same color. A new value glides in rather than jumping.
type ProgressBar struct {
	label   string
	color   theme.Style
	target  float64
	shown   float64
	stamp   time.Time
	glide   time.Duration
	percent bool
}

// NewProgressBar returns an empty bar in color, a foreground style such as
// theme.RGB(177, 185, 249); one the palette of the terminal decides draws
// in reverse video.
func NewProgressBar(label string, color theme.Style) *ProgressBar {
	return &ProgressBar{label: label, color: color, glide: 400 * time.Millisecond, percent: true}
}

// SetLabel sets the text inside the bar.
func (p *ProgressBar) SetLabel(label string) *ProgressBar { p.label = label; return p }

// SetColor sets the color of the bar.
func (p *ProgressBar) SetColor(color theme.Style) *ProgressBar { p.color = color; return p }

// SetPercent shows or hides the percentage on the right.
func (p *ProgressBar) SetPercent(on bool) *ProgressBar { p.percent = on; return p }

// SetValue sets how full the bar is, from 0 to 1; the bar glides there.
func (p *ProgressBar) SetValue(v float64) *ProgressBar {
	p.advance()
	p.target = min(max(v, 0), 1)
	return p
}

// Value is the value last set.
func (p *ProgressBar) Value() float64 { return p.target }

// advance moves the value shown toward the target, as time passed.
func (p *ProgressBar) advance() {
	now := time.Now()
	if !p.stamp.IsZero() && p.glide > 0 {
		k := 1 - math.Exp(-float64(now.Sub(p.stamp))/float64(p.glide))
		p.shown += (p.target - p.shown) * k
	}
	if math.Abs(p.target-p.shown) < 0.002 || p.glide <= 0 {
		p.shown = p.target
	}
	p.stamp = now
}

// Animating reports whether the bar still glides.
func (p *ProgressBar) Animating() bool { return p.shown != p.target }

// Draw draws the bar across the canvas.
func (p *ProgressBar) Draw(c Canvas) {
	p.advance()
	w := c.Width()
	if w <= 0 {
		return
	}
	suffix := ""
	if p.percent {
		suffix = " " + strconv.Itoa(int(math.Round(p.target*100))) + "% "
		suffix = strings.Repeat(" ", max(6-len(suffix), 0)) + suffix
	}
	room := max(w-textWidth(suffix)-1, 0)
	text := " " + truncate(oneLine(p.label), room)
	text += strings.Repeat(" ", max(w-textWidth(text)-textWidth(suffix), 0)) + suffix
	filled := int(math.Round(p.shown * float64(w)))
	fill, track := theme.Join(p.color, theme.Reverse, theme.Bold), theme.Join(p.color)
	if fg, _, _ := colors(p.color); fg.known {
		fill = theme.Join(paint(fg.c, true), paint(scale(fg.c, 0.22), false))
		track = theme.Join(paint(scale(fg.c, 0.42), true), paint(fg.c, false))
	}
	x := 0
	for _, g := range graphemes(text) {
		style := track
		if x < filled {
			style = fill
		}
		if x >= w-len(suffix) {
			style = theme.Join(style, theme.Bold)
		}
		x += c.Text(x, 0, g.text, style)
	}
}

// Size is one line across.
func (*ProgressBar) Size(width, _ int) (int, int) { return width, 1 }

// scale is c darkened to the fraction f of its brightness.
func scale(c rgb, f float64) rgb {
	ch := func(v uint8) uint8 { return uint8(math.Round(float64(v) * f)) }
	return rgb{ch(c.r), ch(c.g), ch(c.b)}
}

// PastelColors are light colors that tell apart bars and badges side by
// side, as the agents of a live view are told apart.
var PastelColors = []theme.Style{
	theme.RGB(177, 185, 249), // periwinkle
	theme.RGB(166, 209, 137), // green
	theme.RGB(244, 184, 228), // pink
	theme.RGB(129, 200, 190), // teal
	theme.RGB(198, 160, 246), // lavender
	theme.RGB(238, 212, 159), // yellow
	theme.RGB(138, 173, 244), // blue
	theme.RGB(250, 157, 169), // rose
	theme.RGB(121, 247, 199), // mint
	theme.RGB(100, 208, 250), // sky
}

// animating reports whether a widget in the tree of root is Animating.
func animating(root Widget) bool {
	if a, ok := root.(Animated); ok && a.Animating() {
		return true
	}
	if c, ok := root.(Container); ok {
		for _, child := range c.Children() {
			if child != nil && animating(child) {
				return true
			}
		}
	}
	return false
}
