package term

import (
	"strconv"
	"strings"
)

// sgrReset ends every SGR attribute.
const sgrReset = "\x1b[0m"

// The attributes a rendition tracks, each in a slot of its own.
const (
	slotBold = iota
	slotDim
	slotItalic
	slotUnderline
	slotBlink
	slotReverse
	slotStrike
	slotOverline
	slotForeground
	slotBackground
	slotCount
)

// slotReset is the slot of a parameter that resets every attribute.
const slotReset = -1

// clearCodes are the parameters that switch each slot's attribute off. Bold
// and dim share theirs.
var clearCodes = [slotCount]string{"22", "22", "23", "24", "25", "27", "29", "55", "39", "49"}

// sgrParam is one attribute an SGR sequence sets or switches off: its
// parameters as they are kept ("1", "38;5;196"), and the slot it acts on.
type sgrParam struct {
	text  string
	slot  int
	clear bool
}

// sgrParams returns the parameters of seq when it is an SGR sequence: ESC [,
// digits and semicolons, then m. A sequence with anything else in it, such as
// the ">" of CSI > 4 ; 1 m, which sets keyboard modes, is not one.
func sgrParams(seq string) (string, bool) {
	params, ok := strings.CutPrefix(seq, "\x1b[")
	if !ok {
		return "", false
	}
	params, ok = strings.CutSuffix(params, "m")
	if !ok {
		return "", false
	}
	for i := 0; i < len(params); i++ {
		if (params[i] < '0' || params[i] > '9') && params[i] != ';' {
			return "", false
		}
	}
	return params, true
}

// parseSGR reads the parameters of an SGR sequence and returns the ones that
// set or clear an attribute a rendition tracks, and whether they differ from
// what the sequence wrote: a parameter was left out, or a 24-bit color became
// one of the 256. An empty parameter is 0, a reset, as terminals read it.
func parseSGR(params string) (kept []sgrParam, changed bool) {
	fields := strings.Split(params, ";")
	for i := 0; i < len(fields); i++ {
		code := 0
		if fields[i] != "" {
			n, err := strconv.Atoi(fields[i])
			if err != nil {
				changed = true
				continue
			}
			code = n
		}
		param := sgrParam{text: fields[i]}
		switch {
		case code == 0:
			param.slot = slotReset
		case code == 1:
			param.slot = slotBold
		case code == 2:
			param.slot = slotDim
		case code == 3:
			param.slot = slotItalic
		case code == 4, code == 21:
			param.slot = slotUnderline
		case code == 5, code == 6:
			param.slot = slotBlink
		case code == 7:
			param.slot = slotReverse
		case code == 9:
			param.slot = slotStrike
		case code == 22:
			param.slot, param.clear = slotBold, true
		case code == 23:
			param.slot, param.clear = slotItalic, true
		case code == 24:
			param.slot, param.clear = slotUnderline, true
		case code == 25:
			param.slot, param.clear = slotBlink, true
		case code == 27:
			param.slot, param.clear = slotReverse, true
		case code == 29:
			param.slot, param.clear = slotStrike, true
		case code >= 30 && code <= 37, code >= 90 && code <= 97:
			param.slot = slotForeground
		case code == 39:
			param.slot, param.clear = slotForeground, true
		case code >= 40 && code <= 47, code >= 100 && code <= 107:
			param.slot = slotBackground
		case code == 49:
			param.slot, param.clear = slotBackground, true
		case code == 53:
			param.slot = slotOverline
		case code == 55:
			param.slot, param.clear = slotOverline, true
		case code == 38, code == 48, code == 58:
			// An extended color's components are read together: the 2 of
			// 38;2;… is a selector, not a dim attribute. An underline color
			// (58) goes with its components, so that none of them is left
			// behind as an attribute.
			n, index, exact := extendedColor(fields[i+1:])
			i += n
			if index < 0 || code == 58 {
				changed = true
				continue
			}
			param.text += ";5;" + strconv.Itoa(index)
			param.slot = slotForeground
			if code == 48 {
				param.slot = slotBackground
			}
			changed = changed || !exact
		default:
			changed = true
			continue
		}
		kept = append(kept, param)
	}
	return kept, changed
}

// extendedColor reads the components of an extended color, what follows 38,
// 48 or 58: 5 and a palette index, or 2 and red, green and blue, each 0 to
// 255. It returns how many fields belong to the color, the index of the
// color among the 256, or -1 when the fields make no valid color, and
// whether that index is what the fields wrote: a 24-bit color is given the
// index of the nearest of the 256.
func extendedColor(fields []string) (n, index int, exact bool) {
	if len(fields) == 0 {
		return 0, -1, false
	}
	want := 0
	switch fields[0] {
	case "5":
		want = 1
	case "2":
		want = 3
	default:
		return 1, -1, false
	}
	if len(fields) < 1+want {
		return len(fields), -1, false
	}
	var components [3]int
	for i, field := range fields[1 : 1+want] {
		value, err := strconv.Atoi(field)
		if err != nil || value < 0 || value > 255 {
			return 1 + want, -1, false
		}
		components[i] = value
	}
	if want == 1 {
		// Leading zeros are not what a rendition carries on: 38;5;007 is
		// written again as 38;5;7.
		return 1 + want, components[0], fields[1] == strconv.Itoa(components[0])
	}
	return 1 + want, nearest256(components[0], components[1], components[2]), false
}

// cubeLevels are the six intensities of each channel in the 6×6×6 color
// cube of the 256 colors, indexes 16 to 231.
var cubeLevels = [6]int{0, 95, 135, 175, 215, 255}

// Nearest256 returns the index among the 256 colors that is nearest to the
// 24-bit color r, g, b, for a terminal that does not take 24-bit colors:
// "38;5;n" paints text in it and "48;5;n" the background. It is the mapping
// [Sanitize] applies to the 24-bit colors of the text it is given.
func Nearest256(r, g, b uint8) int {
	return nearest256(int(r), int(g), int(b))
}

// nearest256 returns the index among the 256 colors that is nearest to the
// 24-bit color r, g, b: a color of the cube, or one of the 24 grays, indexes
// 232 to 255, where that is nearer. The 16 colors below the cube are left
// out, since every terminal has its own shades for them.
func nearest256(r, g, b int) int {
	ri, gi, bi := cubeIndex(r), cubeIndex(g), cubeIndex(b)
	cube := 16 + 36*ri + 6*gi + bi
	cubeDistance := distance(cubeLevels[ri], cubeLevels[gi], cubeLevels[bi], r, g, b)
	if cubeDistance == 0 {
		return cube
	}
	// The grays run from 8 to 238 in steps of 10.
	step := min(max((r+g+b)/3-3, 0)/10, 23)
	gray := 8 + 10*step
	if distance(gray, gray, gray, r, g, b) < cubeDistance {
		return 232 + step
	}
	return cube
}

// cubeIndex returns which of the cube's six levels is nearest to v, 0 to
// 255.
func cubeIndex(v int) int {
	switch {
	case v < 48:
		return 0
	case v < 115:
		return 1
	}
	return (v - 35) / 40
}

// distance is the squared distance between two colors.
func distance(r1, g1, b1, r2, g2, b2 int) int {
	return (r1-r2)*(r1-r2) + (g1-g2)*(g1-g2) + (b1-b2)*(b1-b2)
}

// rendition is the graphic rendition that SGR sequences leave set: for each
// slot the parameters that set it, or nothing for an attribute that is off.
//
// A relative rendition is a change to whatever was set before it rather than
// a state reached from the terminal's default. Switching an attribute off is
// then a change of its own, kept as its clear code, and a reset is kept as
// the first change.
type rendition struct {
	relative bool
	reset    bool
	slots    [slotCount]string
}

// apply changes r the way params change a terminal's rendition.
func (r *rendition) apply(params []sgrParam) {
	for _, param := range params {
		switch {
		case param.slot == slotReset:
			r.slots = [slotCount]string{}
			r.reset = r.relative
		case param.clear:
			value := ""
			if r.relative && !r.reset {
				value = clearCodes[param.slot]
			}
			r.slots[param.slot] = value
			if param.slot == slotBold {
				r.slots[slotDim] = value
			}
		default:
			r.slots[param.slot] = param.text
		}
	}
}

// applyText applies every SGR sequence in s, which holds no escape sequence
// but SGR ones.
func (r *rendition) applyText(s string) {
	for i := 0; i < len(s); {
		n := EscapeLen(s[i:])
		if n == 0 {
			i++
			continue
		}
		if params, ok := sgrParams(s[i : i+n]); ok {
			kept, _ := parseSGR(params)
			r.apply(kept)
		}
		i += n
	}
}

// sequence returns the one SGR sequence that makes r's changes, or "" when r
// changes nothing.
func (r *rendition) sequence() string {
	var params []string
	if r.reset {
		params = append(params, "0")
	}
	bold, dim := r.slots[slotBold], r.slots[slotDim]
	if bold == clearCodes[slotBold] || dim == clearCodes[slotDim] {
		params = append(params, clearCodes[slotBold])
	}
	for slot, value := range r.slots {
		if value == "" || ((slot == slotBold || slot == slotDim) && value == clearCodes[slot]) {
			continue
		}
		params = append(params, value)
	}
	if len(params) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(params, ";") + "m"
}
