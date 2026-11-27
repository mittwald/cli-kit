package tui

import (
	"math"
	"strconv"
	"strings"

	"github.com/mittwald/cli-kit/theme"
)

// minContrast is the contrast ratio, as WCAG defines it, a foreground needs
// against the background it is drawn on. A foreground with less, such as
// muted text on the background of the row under a cursor, is drawn in a
// lighter or darker shade of its own color instead.
const minContrast = 3.0

// rgb is a color in 8-bit sRGB.
type rgb struct{ r, g, b uint8 }

// color is what an SGR color sets: known for a color of the 256-color
// palette from 16 on and for a true color, unknown for the terminal's own.
type color struct {
	known bool
	c     rgb
}

// readable returns style with its foreground in a shade that keeps
// minContrast against its background, when style sets both and they are
// too close. The shade keeps the hue: it is the foreground mixed toward
// white on a dark background and toward black on a light one, as the
// nearest color of the 256-color palette. A style with reverse video, or
// with a color the palette of the terminal decides, is left alone.
func readable(style theme.Style) theme.Style {
	if !strings.Contains(string(style), "48;") {
		return style
	}
	fg, bg, reverse := colors(style)
	if reverse || !fg.known || !bg.known || contrast(fg.c, bg.c) >= minContrast {
		return style
	}
	toward := rgb{255, 255, 255}
	if luminance(bg.c) > 0.18 {
		toward = rgb{}
	}
	for step := 1; step <= 10; step++ {
		n := nearest256(mix(fg.c, toward, float64(step)/10))
		if contrast(palette256(n), bg.c) >= minContrast {
			return theme.Join(style, theme.Fg256(n))
		}
	}
	return style
}

// colors returns the foreground and the background style ends with, and
// whether it ends in reverse video.
func colors(style theme.Style) (fg, bg color, reverse bool) {
	params := strings.Split(string(style), ";")
	for i := 0; i < len(params); i++ {
		n, err := strconv.Atoi(params[i])
		if err != nil {
			continue
		}
		switch {
		case n == 0:
			fg, bg, reverse = color{}, color{}, false
		case n == 7:
			reverse = true
		case n == 27:
			reverse = false
		case n == 39:
			fg = color{}
		case n == 49:
			bg = color{}
		case n >= 30 && n <= 37, n >= 90 && n <= 97:
			// The 16 colors are the terminal's own, so not known.
			fg = color{}
		case n >= 40 && n <= 47, n >= 100 && n <= 107:
			bg = color{}
		case n == 58:
			// An underline color: skipped, it colors no text.
			_, skip := extended(params[i+1:])
			i += skip
		case n == 38 || n == 48:
			c, skip := extended(params[i+1:])
			i += skip
			if n == 38 {
				fg = c
			} else {
				bg = c
			}
		}
	}
	return fg, bg, reverse
}

// extended reads the 5;n or 2;r;g;b after a 38 or 48 and returns the color
// and how many parameters it took.
func extended(params []string) (color, int) {
	num := func(i int) (uint8, bool) {
		if i >= len(params) {
			return 0, false
		}
		n, err := strconv.ParseUint(params[i], 10, 8)
		return uint8(n), err == nil
	}
	if len(params) == 0 {
		return color{}, 0
	}
	switch params[0] {
	case "5":
		n, ok := num(1)
		if !ok {
			return color{}, min(2, len(params))
		}
		if n < 16 {
			return color{}, 2
		}
		return color{known: true, c: palette256(n)}, 2
	case "2":
		r, okR := num(1)
		g, okG := num(2)
		b, okB := num(3)
		if !okR || !okG || !okB {
			return color{}, min(4, len(params))
		}
		return color{known: true, c: rgb{r, g, b}}, 4
	}
	return color{}, 0
}

// cubeLevels are the six levels of each channel in the 6×6×6 color cube of
// the 256-color palette.
var cubeLevels = [6]uint8{0, 95, 135, 175, 215, 255}

// palette256 is color n of the 256-color palette, n at least 16: the 16
// below are the terminal's own.
func palette256(n uint8) rgb {
	if n >= 232 {
		v := 8 + 10*(n-232)
		return rgb{v, v, v}
	}
	n -= 16
	return rgb{cubeLevels[n/36], cubeLevels[n/6%6], cubeLevels[n%6]}
}

// nearest256 is the color of the 256-color palette, from 16 on, closest to
// c.
func nearest256(c rgb) uint8 {
	best, bestD := uint8(16), math.MaxInt
	for n := 16; n < 256; n++ {
		p := palette256(uint8(n))
		dr, dg, db := int(p.r)-int(c.r), int(p.g)-int(c.g), int(p.b)-int(c.b)
		if d := dr*dr + dg*dg + db*db; d < bestD {
			best, bestD = uint8(n), d
		}
	}
	return best
}

// mix is c moved the fraction t of the way to toward.
func mix(c, toward rgb, t float64) rgb {
	ch := func(a, b uint8) uint8 { return uint8(math.Round(float64(a) + (float64(b)-float64(a))*t)) }
	return rgb{ch(c.r, toward.r), ch(c.g, toward.g), ch(c.b, toward.b)}
}

// luminance is the relative luminance of c, as WCAG defines it.
func luminance(c rgb) float64 {
	lin := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.r) + 0.7152*lin(c.g) + 0.0722*lin(c.b)
}

// contrast is the contrast ratio of a and b, as WCAG defines it: from 1
// for the same luminance to 21 for black on white.
func contrast(a, b rgb) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// background is the part of style that sets the background, so joining it
// after another style keeps the background of style, whatever the other
// sets, as the row under a cursor does.
func background(style theme.Style) theme.Style {
	params := strings.Split(string(style), ";")
	var bg []string
	for i := 0; i < len(params); i++ {
		switch p := params[i]; {
		case p == "48":
			_, skip := extended(params[i+1:])
			bg = params[i : i+1+skip]
			i += skip
		case p == "38" || p == "58":
			_, skip := extended(params[i+1:])
			i += skip
		case p == "0" || p == "49":
			bg = nil
		case len(p) == 2 && p[0] == '4' && p[1] <= '7', len(p) == 3 && strings.HasPrefix(p, "10") && p[2] <= '7':
			bg = params[i : i+1]
		}
	}
	return theme.Style(strings.Join(bg, ";"))
}

// Solid returns a style that paints the foreground color of style as the
// background, with near-black or white text on it, whichever reads better,
// in bold: the look of a badge or a bar that stands out, such as
// Badge("internal", Solid(theme.Fg256(214))). Where style has no color of
// the 256-color palette or a true color, Solid is style in reverse video.
func Solid(style theme.Style) theme.Style {
	fg, _, _ := colors(style)
	if !fg.known {
		return theme.Join(style, theme.Bold, theme.Reverse)
	}
	text := theme.Fg256(255)
	if contrast(rgb{28, 28, 28}, fg.c) > contrast(rgb{255, 255, 255}, fg.c) {
		text = theme.Fg256(234)
	}
	return theme.Join(theme.Bold, paint(fg.c, true), text)
}
