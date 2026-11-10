package progress

import (
	"math"
	"strconv"
)

// Color is a 24-bit RGB color.
type Color struct{ R, G, B uint8 }

// Palette holds the colors a [Bar] is drawn in.
type Palette struct {
	// Gradient holds the colors the filled part of the bar passes through
	// from left to right. A single color fills evenly; an empty Gradient
	// uses the default one.
	Gradient []Color
	// Track is the background of the part not yet filled.
	Track Color
	// TrackText is the label color on the part not yet filled.
	TrackText Color
}

// DefaultPalette is a cool gradient from steel blue through violet to teal,
// on a dark slate track. Every gradient color stays dark enough to carry a
// light tint of its own hue at the default text contrast, so the label never
// flips to dark text in the middle of a word.
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

// sgr returns the SGR parameters that paint text in fg on bg.
func sgr(bg, fg Color) string {
	return "48;2;" + rgb(bg) + ";38;2;" + rgb(fg)
}

func rgb(c Color) string {
	return strconv.Itoa(int(c.R)) + ";" + strconv.Itoa(int(c.G)) + ";" + strconv.Itoa(int(c.B))
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
// hue and saturation, lightened only as far as reaching contrast requires.
// It always lightens. Choosing light or dark text per cell would flip the
// label in the middle of a word wherever the gradient crosses mid-luminance.
func textColor(background Color, contrast float64) Color {
	const steps = 16
	base := toHSL(background)
	low, high := 0.0, 1.0
	for range steps {
		mid := (low + high) / 2
		candidate := base
		candidate.lightness += (1 - candidate.lightness) * mid
		if contrastRatio(background, fromHSL(candidate)) >= contrast {
			high = mid
		} else {
			low = mid
		}
	}
	base.lightness += (1 - base.lightness) * high
	return fromHSL(base)
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
