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
	slotUnderlineColor
	slotCount
)

// slotReset is the slot of a parameter that resets every attribute.
const slotReset = -1

// clearCodes are the parameters that switch each slot's attribute off. Bold
// and dim share theirs.
var clearCodes = [slotCount]string{"22", "22", "23", "24", "25", "27", "29", "55", "39", "49", "59"}

// sgrParam is one attribute an SGR sequence sets or switches off: its
// parameters as written ("1", "38;5;196"), and the slot it acts on.
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
// set or clear an attribute a rendition tracks, and whether it left any out.
// An empty parameter is 0, a reset, as terminals read it.
func parseSGR(params string) (kept []sgrParam, dropped bool) {
	fields := strings.Split(params, ";")
	for i := 0; i < len(fields); i++ {
		code := 0
		if fields[i] != "" {
			n, err := strconv.Atoi(fields[i])
			if err != nil {
				dropped = true
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
		case code == 59:
			param.slot, param.clear = slotUnderlineColor, true
		case code == 38, code == 48, code == 58:
			// An extended color's components are read together: the 2 of
			// 38;2;… is a selector, not a dim attribute.
			n, ok := extendedColor(fields[i+1:])
			param.text = strings.Join(fields[i:i+1+n], ";")
			i += n
			if !ok {
				dropped = true
				continue
			}
			param.slot = extendedSlot(code)
		default:
			dropped = true
			continue
		}
		kept = append(kept, param)
	}
	return kept, dropped
}

// extendedSlot is the slot an extended color sets: 38 the foreground, 48
// the background, 58 the underline.
func extendedSlot(code int) int {
	switch code {
	case 48:
		return slotBackground
	case 58:
		return slotUnderlineColor
	}
	return slotForeground
}

// extendedColor reads the components of an extended color, what follows 38,
// 48 or 58: 5 and a palette index, or 2 and red, green and blue, each 0 to
// 255. It returns how many fields belong to the color and whether they make
// a valid one.
func extendedColor(fields []string) (int, bool) {
	if len(fields) == 0 {
		return 0, false
	}
	want := 0
	switch fields[0] {
	case "5":
		want = 1
	case "2":
		want = 3
	default:
		return 1, false
	}
	if len(fields) < 1+want {
		return len(fields), false
	}
	for _, field := range fields[1 : 1+want] {
		if n, err := strconv.Atoi(field); err != nil || n < 0 || n > 255 {
			return 1 + want, false
		}
	}
	return 1 + want, true
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
