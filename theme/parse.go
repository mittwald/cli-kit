package theme

import (
	"strconv"
	"strings"

	"github.com/mittwald/cli-kit/term"
)

// attributeNames are the text attributes a style specification can name.
var attributeNames = map[string]Style{
	"bold": Bold, "b": Bold,
	"fuzzy": Dim, "dim": Dim,
	"italic": Italic, "i": Italic,
	"underline": Underline, "u": Underline,
	"blink":   "5",
	"reverse": Reverse, "invert": Reverse,
	"strikethrough": "9",
}

// colorNames are the named colors, as their foreground SGR parameter.
var colorNames = map[string]int{
	"black": 30, "red": 31, "green": 32, "yellow": 33, "blue": 34,
	"magenta": 35, "purple": 35, "cyan": 36, "white": 37, "gray": 37, "grey": 37,
	"darkgray": 90, "darkgrey": 90, "lightred": 91, "lightgreen": 92, "lime": 92,
	"lightyellow": 93, "lightblue": 94, "lightmagenta": 95, "lightcyan": 96, "lightwhite": 97,
}

// ParseStyle reads a style specification, such as a user puts into an
// environment variable or a configuration file to change a color: parts
// separated by colons, each a color or a text attribute, as in "245",
// "160:bold" or "fg=white:bg=#aa0000:underline".
//
// A color is a name (black, red, green, yellow, blue, magenta or purple,
// cyan, white or gray, and darkgray, lightred, lightgreen or lime,
// lightyellow, lightblue, lightmagenta, lightcyan and lightwhite), a number
// from 0 to 255 of the 256-color palette, or a hex color of three or six
// digits with an optional "#" or "0x". It colors the text; prefixed with
// "bg=" or "background=" it colors the background, and "fg=" or
// "foreground=" says the default out loud. A number of one to three digits
// is a palette index while it is at most 255; a larger one, such as 300,
// is read as a hex color of three digits.
//
// A hex color is painted as it is where term.TrueColor reports that the
// terminal takes 24-bit colors, and as the nearest of the 256 colors
// (term.Nearest256) everywhere else, since a terminal that does not know
// 24-bit colors reads their parameters as attributes of their own.
//
// An attribute is one of bold (or b), fuzzy (or dim), italic (or i),
// underline (or u), blink, reverse (or invert) and strikethrough. Upper and
// lower case are the same, and white space around a part is ignored.
//
// Parts that are neither are skipped. The result is false when nothing is
// left, so a mistyped specification paints nothing rather than something
// unexpected. The result holds only SGR parameters, never anything of the
// specification itself.
func ParseStyle(spec string) (Style, bool) {
	var parts []Style
	for token := range strings.SplitSeq(spec, ":") {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == "" {
			continue
		}
		if style, ok := attributeNames[token]; ok {
			parts = append(parts, style)
			continue
		}
		background := false
		if key, value, found := strings.Cut(token, "="); found {
			switch strings.TrimSpace(key) {
			case "fg", "foreground":
			case "bg", "background":
				background = true
			default:
				continue
			}
			token = strings.TrimSpace(value)
		}
		if style, ok := colorStyle(token, background); ok {
			parts = append(parts, style)
		}
	}
	style := Join(parts...)
	return style, style != ""
}

// colorStyle returns the style for one color of a specification.
func colorStyle(value string, background bool) (Style, bool) {
	if code, ok := colorNames[value]; ok {
		if background {
			code += 10
		}
		return Style(strconv.Itoa(code)), true
	}
	if len(value) >= 1 && len(value) <= 3 && allDigits(value) {
		// A number past 255 is no palette index; it may still be a hex color.
		if index, err := strconv.ParseUint(value, 10, 8); err == nil {
			if background {
				return Bg256(uint8(index)), true
			}
			return Fg256(uint8(index)), true
		}
	}
	r, g, b, ok := hexColor(value)
	if !ok {
		return "", false
	}
	switch {
	case term.TrueColor() && background:
		return BgRGB(r, g, b), true
	case term.TrueColor():
		return RGB(r, g, b), true
	case background:
		return Bg256(uint8(term.Nearest256(r, g, b))), true //nolint:gosec // Nearest256 returns an index from 16 to 255
	}
	return Fg256(uint8(term.Nearest256(r, g, b))), true //nolint:gosec // Nearest256 returns an index from 16 to 255
}

// hexColor reads a hex color of three or six lower-case digits, with an
// optional "#" or "0x" in front.
func hexColor(value string) (r, g, b uint8, ok bool) {
	digits, found := strings.CutPrefix(value, "#")
	if !found {
		digits, _ = strings.CutPrefix(value, "0x")
	}
	if len(digits) == 3 {
		digits = string([]byte{digits[0], digits[0], digits[1], digits[1], digits[2], digits[2]})
	}
	if len(digits) != 6 {
		return 0, 0, 0, false
	}
	var channels [3]uint8
	for i := range channels {
		channel, err := strconv.ParseUint(digits[2*i:2*i+2], 16, 8)
		if err != nil {
			return 0, 0, 0, false
		}
		channels[i] = uint8(channel)
	}
	return channels[0], channels[1], channels[2], true
}

// allDigits reports whether s holds nothing but ASCII digits.
func allDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
