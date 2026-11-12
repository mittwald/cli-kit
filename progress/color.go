package progress

import (
	"math"
	"strconv"
	"strings"

	"github.com/mittwald/cli-kit/term"
)

// Color is a 24-bit RGB color.
type Color struct{ R, G, B uint8 }

// Palette holds the colors a [Bar] is drawn in.
type Palette struct {
	// Gradient holds the colors the filled part of the bar passes through
	// from left to right. A single color fills evenly; an empty Gradient
	// uses the default one.
	//
	// The label on the gradient is a tint of the color behind it: lighter
	// when every color of the gradient is dark enough for light text to
	// reach Options.TextContrast, darker when every color is light enough
	// for dark text to reach it. The whole label goes the same way, so it
	// does not flip in the middle of a word. A gradient that runs from dark
	// colors to light ones leaves no way that reaches the contrast on all
	// of it: the label then goes the way that comes closest, and is hard to
	// read on a part of the bar. Keep a gradient on one side.
	Gradient []Color
	// Track is the background of the part not yet filled.
	Track Color
	// TrackText is the label color on the part not yet filled.
	TrackText Color
}

// DefaultPalette is a cool gradient from steel blue through violet to teal,
// on a dark slate track. Every gradient color stays dark enough to carry a
// light tint of its own hue at the default text contrast.
var DefaultPalette = Palette{
	Gradient: []Color{
		{R: 54, G: 86, B: 140},  // deep steel blue
		{R: 82, G: 92, B: 173},  // indigo
		{R: 115, G: 77, B: 183}, // violet
		{R: 153, G: 69, B: 140}, // orchid
		{R: 44, G: 110, B: 145}, // sky blue
		{R: 43, G: 119, B: 101}, // teal
	},
	Track:     Color{R: 52, G: 58, B: 69},
	TrackText: Color{R: 160, G: 167, B: 177},
}

// sgr returns the SGR parameters that paint text in fg on bg: in 24-bit
// colors where the terminal takes them, otherwise in the nearest of the 256
// colors.
func sgr(bg, fg Color, truecolor bool) string {
	if !truecolor {
		return "48;5;" + strconv.Itoa(term.Nearest256(bg.R, bg.G, bg.B)) +
			";38;5;" + strconv.Itoa(term.Nearest256(fg.R, fg.G, fg.B))
	}
	return "48;2;" + rgb(bg) + ";38;2;" + rgb(fg)
}

func rgb(c Color) string {
	return strconv.Itoa(int(c.R)) + ";" + strconv.Itoa(int(c.G)) + ";" + strconv.Itoa(int(c.B))
}

// truecolor reports whether colorterm, the value of the COLORTERM
// environment variable, announces a terminal that takes 24-bit colors.
// Terminals that do not take them read the parameters of a 24-bit color as
// attributes of their own, so the bar paints them only where they are
// announced.
func truecolor(colorterm string) bool {
	return strings.EqualFold(colorterm, "truecolor") || strings.EqualFold(colorterm, "24bit")
}

// gradientAt returns the gradient color of column out of width columns.
func gradientAt(gradient []Color, column, width int) Color {
	if len(gradient) == 1 || width <= 1 || column <= 0 {
		return gradient[0]
	}
	column = min(column, width-1)
	position := float64(column) / float64(width-1) * float64(len(gradient)-1)
	start := min(int(position), len(gradient)-2)
	return mix(gradient[start], gradient[start+1], position-float64(start))
}

func mix(a, b Color, fraction float64) Color {
	channel := func(start, end uint8) uint8 {
		return uint8(float64(start) + (float64(end)-float64(start))*fraction + 0.5)
	}
	return Color{R: channel(a.R, b.R), G: channel(a.G, b.G), B: channel(a.B, b.B)}
}

type hsl struct{ hue, saturation, lightness float64 }

// textColor returns the label color for background: the background's own
// hue and saturation, made lighter, or darker with darker set, only as far
// as reaching contrast requires. A background that cannot carry the
// contrast that way gets white or black text.
//
// The direction is the caller's, the same for a whole gradient, see
// darkerText. Choosing light or dark text per cell would flip the label in
// the middle of a word wherever the gradient crosses mid-luminance.
func textColor(background Color, contrast float64, darker bool) Color {
	const steps = 16
	base := toHSL(background)
	target := 1.0
	if darker {
		target = 0
	}
	low, high := 0.0, 1.0
	for range steps {
		mid := (low + high) / 2
		candidate := base
		candidate.lightness += (target - candidate.lightness) * mid
		if contrastRatio(background, fromHSL(candidate)) >= contrast {
			high = mid
		} else {
			low = mid
		}
	}
	base.lightness += (target - base.lightness) * high
	return fromHSL(base)
}

// darkerText decides which way the label on gradient goes: lighter than
// the color behind it when light text reaches contrast on every color the
// gradient passes through, as it does on the dark colors a bar usually has,
// and darker when it does not and dark text gets closer to contrast on the
// color that suits it least.
func darkerText(gradient []Color, contrast float64) bool {
	white, black := Color{R: 255, G: 255, B: 255}, Color{}
	onLight, onDark := math.Inf(1), math.Inf(1)
	measure := func(c Color) {
		onLight = min(onLight, contrastRatio(c, white))
		onDark = min(onDark, contrastRatio(c, black))
	}
	const between = 8 // colors looked at from one gradient color to the next
	for i, c := range gradient {
		measure(c)
		if i+1 == len(gradient) {
			break
		}
		for step := 1; step < between; step++ {
			measure(mix(c, gradient[i+1], float64(step)/between))
		}
	}
	return onLight < contrast && onDark > onLight
}

func toHSL(c Color) hsl {
	r := float64(c.R) / 255
	g := float64(c.G) / 255
	b := float64(c.B) / 255
	maximum := max(r, g, b)
	minimum := min(r, g, b)
	delta := maximum - minimum
	lightness := (maximum + minimum) / 2
	if delta == 0 {
		return hsl{lightness: lightness}
	}
	saturation := delta / (1 - math.Abs(2*lightness-1))
	var hue float64
	switch maximum {
	case r:
		hue = math.Mod((g-b)/delta, 6)
	case g:
		hue = (b-r)/delta + 2
	default:
		hue = (r-g)/delta + 4
	}
	hue /= 6
	if hue < 0 {
		hue++
	}
	return hsl{hue: hue, saturation: saturation, lightness: lightness}
}

func fromHSL(c hsl) Color {
	chroma := (1 - math.Abs(2*c.lightness-1)) * c.saturation
	hue := c.hue * 6
	x := chroma * (1 - math.Abs(math.Mod(hue, 2)-1))
	var r, g, b float64
	switch {
	case hue < 1:
		r, g = chroma, x
	case hue < 2:
		r, g = x, chroma
	case hue < 3:
		g, b = chroma, x
	case hue < 4:
		g, b = x, chroma
	case hue < 5:
		r, b = x, chroma
	default:
		r, b = chroma, x
	}
	match := c.lightness - chroma/2
	channel := func(v float64) uint8 {
		return uint8(math.Max(0, math.Min(255, math.Round((v+match)*255))))
	}
	return Color{R: channel(r), G: channel(g), B: channel(b)}
}

// contrastRatio is the WCAG 2 contrast ratio of two colors, from 1 to 21.
func contrastRatio(a, b Color) float64 {
	lighter := relativeLuminance(a)
	darker := relativeLuminance(b)
	if lighter < darker {
		lighter, darker = darker, lighter
	}
	return (lighter + 0.05) / (darker + 0.05)
}

func relativeLuminance(c Color) float64 {
	channel := func(value uint8) float64 {
		v := float64(value) / 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}
