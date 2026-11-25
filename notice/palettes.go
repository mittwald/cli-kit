package notice

import (
	"strconv"
	"strings"

	"github.com/mittwald/cli-kit/theme"
)

// Default are the default colors. ERROR and OK badges take the terminal's
// own red and bright green, so they follow its color scheme; everything
// else is fixed.
var Default = Colors{
	Dark: palette(
		[5]string{"#5FAFD7", "#ECB44A", "256:252", "256:218", "256:105"}, "256:244",
		levelSpec{LevelError, "256:203", "41", "97", "#5A1818", "97", [5]string{"#FFE28C", "#FFE28C", "#FFE28C", "#FFD6EC", "#D9D2FF"}},
		levelSpec{LevelWarn, "256:221", "#ECB44A", "30", "#54421C", "97", [5]string{"#14326E", "#781E00", "#14326E", "#6A0E3E", "#3A1060"}},
		levelSpec{LevelInfo, "#ADB4C0", "#5FAFD7", "30", "#1E3A4C", "97", [5]string{"#14326E", "#781E00", "#50145A", "#6A0E3E", "#3A1060"}},
		levelSpec{LevelOK, "256:118", "102", "30", "#1C4622", "97", [5]string{"#14326E", "#781E00", "#14326E", "#6A0E3E", "#3A1060"}},
		levelSpec{LevelDebug, "256:242", "#555D6B", "97", "#30343C", "97", [5]string{"#FFE28C", "#FFE28C", "#FFE28C", "#FFD6EC", "#D9D2FF"}},
	),
	Light: lightPalette(
		[5]string{"#1F5F99", "#9A5B00", "#303030", "#A0306E", "#5B3FB5"}, "#8A8F98",
		lightSpec{LevelError, "#C0392B", "#FBE0E0", "#5A1010"},
		lightSpec{LevelWarn, "#A66B00", "#FBEBC8", "#4A3300"},
		lightSpec{LevelInfo, "#4A5868", "#DCEEF8", "#0F3550"},
		lightSpec{LevelOK, "#2E7D32", "#DDF2DD", "#14451A"},
		lightSpec{LevelDebug, "#8A8F98", "#ECEEF1", "#2E333C"},
	),
}

// Muted are softer colors than [Default].
var Muted = Colors{
	Dark: palette(
		[5]string{"#7FC4EA", "#F0C36A", "#E4E7EC", "#E6A6C8", "#A99BE0"}, "#7A8290",
		levelSpec{LevelError, "#E07A7A", "#B83A3A", "#FBEAEA", "#4A1C1C", "#FBEAEA", [5]string{"#BFE3FF", "#FFD27A", "#FFFFFF", "#FFD6EC", "#E2DAFF"}},
		levelSpec{LevelWarn, "#E6B85C", "#D9A43F", "#1F1608", "#4A3814", "#F6EBD3", [5]string{"#0F3A73", "#7A1F00", "#3B1A55", "#6A0E3E", "#4A1A6A"}},
		levelSpec{LevelInfo, "#7FB2D6", "#4C8DB8", "#F2F8FC", "#1C3448", "#F2F8FC", [5]string{"#FFE7A8", "#FFC37A", "#FFFFFF", "#FFE0F0", "#E9DDFF"}},
		levelSpec{LevelOK, "#6CBF83", "#3F9A57", "#F1FAF3", "#1A3E24", "#F1FAF3", [5]string{"#D9F0FF", "#FFE08A", "#FFFFFF", "#FFE0F0", "#E9DDFF"}},
		levelSpec{LevelDebug, "#7A8290", "#4A5160", "#E4E7EC", "#2C3038", "#E4E7EC", [5]string{"#9FD3F0", "#F0C36A", "#FFFFFF", "#FFD6EC", "#D9D2FF"}},
	),
	Light: lightPalette(
		[5]string{"#2A6496", "#8C5A00", "#333333", "#9B3A72", "#5B4BA8"}, "#8A9099",
		lightSpec{LevelError, "#A33A3A", "#F6E1E1", "#4A1414"},
		lightSpec{LevelWarn, "#9A6E14", "#F7ECD4", "#3F2E08"},
		lightSpec{LevelInfo, "#3B6E91", "#DFEBF4", "#13304A"},
		lightSpec{LevelOK, "#2F7A45", "#DFF0E4", "#123A1E"},
		lightSpec{LevelDebug, "#7A8290", "#E9EBEF", "#2C3038"},
	),
}

// Deep are dark backgrounds with the table tones of a timesheet as values.
var Deep = Colors{
	Dark: palette(
		[5]string{"#8FD0F2", "#F2BD55", "#FFFFFF", "#F2A6D0", "#B7A6FF"}, "#6B7484",
		levelSpec{LevelError, "#E06C6C", "#6E1C1C", "#F4DADA", "#3E1010", "#F4DADA", deepValues},
		levelSpec{LevelWarn, "#F2BD55", "#5E4512", "#F6EBD3", "#3A2A0A", "#F6EBD3", [5]string{"#7FC4EA", "#FFD36B", "#FFFFFF", "#F5B8DA", "#C7B8FF"}},
		levelSpec{LevelInfo, "#8FD0F2", "#1D3B53", "#DCEAF4", "#102434", "#DCEAF4", [5]string{"#8FD0F2", "#F2BD55", "#FFFFFF", "#F5B8DA", "#C7B8FF"}},
		levelSpec{LevelOK, "#77C26E", "#1C4A29", "#DDF0E2", "#102A18", "#DDF0E2", [5]string{"#8FD0F2", "#F2BD55", "#FFFFFF", "#F5B8DA", "#C7B8FF"}},
		levelSpec{LevelDebug, "#6B7484", "#2E333C", "#C9CED6", "#1E2228", "#C9CED6", deepValues},
	),
	Light: lightPalette(
		[5]string{"#1D4F7A", "#7A4A00", "#202020", "#7A1F55", "#4A2F8F"}, "#6B7484",
		lightSpec{LevelError, "#6E1C1C", "#EAD5D5", "#4A1010"},
		lightSpec{LevelWarn, "#5E4512", "#E8DFC8", "#3A2A0A"},
		lightSpec{LevelInfo, "#1D3B53", "#D3DEE8", "#102434"},
		lightSpec{LevelOK, "#1C4A29", "#D3E3D7", "#102A18"},
		lightSpec{LevelDebug, "#2E333C", "#DADDE2", "#1E2228"},
	),
}

var deepValues = [5]string{"#7FC4EA", "#F2BD55", "#FFFFFF", "#F5B8DA", "#C7B8FF"}

// Pastel are light backgrounds with dark ink, made for light terminals; on
// a dark terminal the labels, values and tints are brightened.
var Pastel = Colors{
	Dark: palette(
		[5]string{"#8FC1F0", "#F0B27A", "#E0E0E0", "#E7A3CB", "#BBA8F0"}, "#8A8F98",
		levelSpec{LevelError, "#F4B4B4", "#F4B4B4", "#3A0D0D", "#4A2E2E", "#F4DADA", [5]string{"#0B3C82", "#8A2A00", "#5A0E0E", "#7A1450", "#4A1A6A"}},
		levelSpec{LevelWarn, "#F6D58E", "#F6D58E", "#3A2A05", "#4A4028", "#F6EBD3", [5]string{"#0B3C82", "#8A2A00", "#4A1A6A", "#7A1450", "#4A1A6A"}},
		levelSpec{LevelInfo, "#B4D8F0", "#B4D8F0", "#0D2638", "#2A3A48", "#DCEAF4", [5]string{"#6A1B78", "#8A2A00", "#0B3C82", "#7A1450", "#4A1A6A"}},
		levelSpec{LevelOK, "#B8E6C2", "#B8E6C2", "#0D2E16", "#2A4232", "#DDF0E2", [5]string{"#0B3C82", "#8A2A00", "#5A1E6A", "#7A1450", "#4A1A6A"}},
		levelSpec{LevelDebug, "#9AA0AA", "#D3D8E0", "#22262E", "#3A3E46", "#D3D8E0", [5]string{"#0B3C82", "#8A2A00", "#3A3F4A", "#7A1450", "#4A1A6A"}},
	),
	Light: lightPalette(
		[5]string{"#0B3C82", "#8A2A00", "#22262E", "#8A1E5A", "#5B2A9A"}, "#6A7080",
		lightSpec{LevelError, "#B03030", "#FBE3E3", "#3A0D0D"},
		lightSpec{LevelWarn, "#9A6A00", "#FCEFD2", "#3A2A05"},
		lightSpec{LevelInfo, "#1F6FA8", "#E3F0F9", "#0D2638"},
		lightSpec{LevelOK, "#2E7D3E", "#E5F5E9", "#0D2E16"},
		lightSpec{LevelDebug, "#6A7080", "#ECEFF3", "#22262E"},
	),
}

// AllColors lists the built-in colors by name.
var AllColors = map[string]Colors{"default": Default, "muted": Muted, "deep": Deep, "pastel": Pastel}

// levelSpec is one level of a dark palette, in the color notation of
// [color]: label, accent, text, tint, tint text, and the block values of
// the built-in roles in the order of [Roles].
type levelSpec struct {
	level                               Level
	label, accent, text, tint, tintText string
	values                              [5]string
}

// lightSpec is what a light palette changes of a level: label, tint and
// tint text.
type lightSpec struct {
	level                 Level
	label, tint, tintText string
}

func palette(values [5]string, separator string, levels ...levelSpec) Palette {
	p := Palette{Levels: make(map[Level]LevelColors, len(levels)), Values: roleStyles(values), Separator: fg(separator)}
	for _, l := range levels {
		p.Levels[l.level] = LevelColors{
			Label: fg(l.label), Accent: bg(l.accent), Text: fg(l.text),
			Tint: bg(l.tint), TintText: fg(l.tintText), Values: roleStyles(l.values),
		}
	}
	return p
}

func lightPalette(values [5]string, separator string, levels ...lightSpec) Palette {
	p := Palette{Levels: make(map[Level]LevelColors, len(levels)), Values: roleStyles(values), Separator: fg(separator)}
	for _, l := range levels {
		p.Levels[l.level] = LevelColors{Label: fg(l.label), Tint: bg(l.tint), TintText: fg(l.tintText)}
	}
	return p
}

func roleStyles(values [5]string) map[Role]theme.Style {
	styles := make(map[Role]theme.Style, len(Roles))
	for i, role := range Roles {
		styles[role] = fg(values[i])
	}
	return styles
}

// fg and bg read the color notation of the built-in palettes: "#rrggbb" for
// a 24-bit color, "256:n" for one of the 256 colors, and a bare SGR
// parameter such as "41" for one of the terminal's own colors.
func fg(spec string) theme.Style { return color(spec, false) }

func bg(spec string) theme.Style { return color(spec, true) }

func color(spec string, background bool) theme.Style {
	switch {
	case strings.HasPrefix(spec, "#"):
		v, err := strconv.ParseUint(spec[1:], 16, 32)
		if err != nil || len(spec) != 7 {
			panic("notice: bad color " + spec)
		}
		r, g, b := uint8(v>>16), uint8(v>>8), uint8(v) //nolint:gosec // masked to a byte by the conversion
		if background {
			return theme.BgRGB(r, g, b)
		}
		return theme.RGB(r, g, b)
	case strings.HasPrefix(spec, "256:"):
		n, err := strconv.ParseUint(spec[4:], 10, 8)
		if err != nil {
			panic("notice: bad color " + spec)
		}
		if background {
			return theme.Bg256(uint8(n))
		}
		return theme.Fg256(uint8(n))
	}
	return theme.Style(spec)
}
