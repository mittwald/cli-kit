package theme

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/mittwald/cli-kit/term"
)

// PaintMatches paints what re matches in text with s, where text may be
// painted already, such as a table cell in a color of its own with a word
// in it to stand out. After each match the attributes the text had set
// before it are switched on again, so the rest of the text keeps its
// colors.
//
// Only text is searched: escape sequences are copied as they are, so a
// match never lands inside one, such as the target of a hyperlink, and a
// match never spans one. Empty matches paint nothing. A nil re paints the
// whole text with s instead, with the colors it had taken out first
// (term.StripANSI), so they do not show through.
//
// It returns text unchanged when s is the zero Style or text is empty. Like
// [Style.Paint] it paints whatever it is given: ask term.Colored, or use
// [Style.For], before painting output that may not go to a terminal. Text
// from elsewhere goes through term.Sanitize or term.Plain first.
func (s Style) PaintMatches(text string, re *regexp.Regexp) string {
	if s == "" || text == "" {
		return text
	}
	if re == nil {
		return s.Paint(term.StripANSI(text))
	}
	var (
		out    strings.Builder
		active sgrState
	)
	segment := func(plain string) {
		if plain == "" {
			return
		}
		resume, reset := active.sequence(), ""
		if resume != "" {
			reset = Reset
		}
		out.WriteString(re.ReplaceAllStringFunc(plain, func(match string) string {
			if match == "" {
				return match
			}
			return reset + s.Paint(match) + resume
		}))
	}
	last := 0
	for i := 0; i < len(text); {
		n := term.EscapeLen(text[i:])
		if n == 0 {
			i++
			continue
		}
		segment(text[last:i])
		sequence := text[i : i+n]
		out.WriteString(sequence)
		if params, ok := sgrParameters(sequence); ok {
			active.apply(params)
		}
		i += n
		last = i
	}
	segment(text[last:])
	return out.String()
}

// sgrParameters returns the parameters of sequence when it is an SGR
// sequence: ESC [, digits, semicolons and the colons of sub-parameters,
// then m.
func sgrParameters(sequence string) (string, bool) {
	params, ok := strings.CutPrefix(sequence, "\x1b[")
	if !ok {
		return "", false
	}
	params, ok = strings.CutSuffix(params, "m")
	if !ok {
		return "", false
	}
	for i := range len(params) {
		if (params[i] < '0' || params[i] > '9') && params[i] != ';' && params[i] != ':' {
			return "", false
		}
	}
	return params, true
}

// The attributes an [sgrState] tracks, each in a slot of its own.
const (
	slotBold = iota
	slotDim
	slotItalic
	slotUnderline
	slotBlink
	slotReverse
	slotConceal
	slotStrike
	slotOverline
	slotForeground
	slotBackground
	slotUnderlineColor
	slotCount
)

// sgrState is what SGR sequences leave switched on: for each attribute the
// parameters that set it, or "" while it is off. It keeps one entry per
// attribute, so a color that replaces another replaces its entry, and the
// sequence that restores the state stays short however much text came
// before.
type sgrState [slotCount]string

// apply changes the state the way an SGR sequence with params changes a
// terminal's. An empty parameter list, and a 0 or an empty parameter, reset
// everything. The components of an extended color, such as the 0 of
// 38;5;0, are read with it, not as parameters of their own. A parameter
// with sub-parameters, such as the color 38:5:196 or the curly underline
// 4:3, is kept as it was written. Parameters that set nothing tracked are
// skipped.
func (st *sgrState) apply(params string) {
	if params == "" {
		*st = sgrState{}
		return
	}
	codes := strings.Split(params, ";")
	for i := 0; i < len(codes); i++ {
		if st.applySub(codes[i]) {
			continue
		}
		code, err := strconv.Atoi(codes[i])
		if codes[i] == "" {
			code, err = 0, nil
		}
		if err != nil {
			continue
		}
		text := strconv.Itoa(code)
		switch {
		case code == 0:
			*st = sgrState{}
		case code == 1:
			st[slotBold] = text
		case code == 2:
			st[slotDim] = text
		case code == 3:
			st[slotItalic] = text
		case code == 4, code == 21:
			st[slotUnderline] = text
		case code == 5, code == 6:
			st[slotBlink] = text
		case code == 7:
			st[slotReverse] = text
		case code == 8:
			st[slotConceal] = text
		case code == 9:
			st[slotStrike] = text
		case code == 22:
			st[slotBold], st[slotDim] = "", ""
		case code == 23:
			st[slotItalic] = ""
		case code == 24:
			st[slotUnderline] = ""
		case code == 25:
			st[slotBlink] = ""
		case code == 27:
			st[slotReverse] = ""
		case code == 28:
			st[slotConceal] = ""
		case code == 29:
			st[slotStrike] = ""
		case code >= 30 && code <= 37, code >= 90 && code <= 97:
			st[slotForeground] = text
		case code == 39:
			st[slotForeground] = ""
		case code >= 40 && code <= 47, code >= 100 && code <= 107:
			st[slotBackground] = text
		case code == 49:
			st[slotBackground] = ""
		case code == 53:
			st[slotOverline] = text
		case code == 55:
			st[slotOverline] = ""
		case code == 59:
			st[slotUnderlineColor] = ""
		case code == 38, code == 48, code == 58:
			n := extendedLen(codes[i+1:])
			slot := slotForeground
			switch code {
			case 48:
				slot = slotBackground
			case 58:
				slot = slotUnderlineColor
			}
			if n > 0 {
				st[slot] = strings.Join(codes[i:i+1+n], ";")
			}
			// An incomplete or unknown color takes its selector along.
			i += max(n, min(1, len(codes)-i-1))
		}
	}
}

// applySub applies a parameter with sub-parameters, such as 38:5:196, and
// reports whether param was one. Colors and underline styles are kept as
// they were written; any other parameter counts as its first part.
func (st *sgrState) applySub(param string) bool {
	head, rest, found := strings.Cut(param, ":")
	if !found {
		return false
	}
	switch strings.TrimLeft(head, "0") {
	case "38":
		st[slotForeground] = param
	case "48":
		st[slotBackground] = param
	case "58":
		st[slotUnderlineColor] = param
	case "4":
		// 4:0 switches the underline off; 4:1 to 4:5 choose its style.
		if strings.Trim(rest, "0") == "" {
			st[slotUnderline] = ""
		} else {
			st[slotUnderline] = param
		}
	default:
		st.apply(head)
	}
	return true
}

// extendedLen returns how many of fields, those after a 38, 48 or 58, make
// up a complete extended color: 5 and an index, or 2 and three components.
// It returns 0 for anything else.
func extendedLen(fields []string) int {
	if len(fields) == 0 {
		return 0
	}
	switch strings.TrimLeft(fields[0], "0") {
	case "5":
		if len(fields) >= 2 {
			return 2
		}
	case "2":
		if len(fields) >= 4 {
			return 4
		}
	}
	return 0
}

// sequence returns the SGR sequence that switches the state on, or "" when
// nothing is on.
func (st *sgrState) sequence() string {
	var params []string
	for _, value := range st {
		if value != "" {
			params = append(params, value)
		}
	}
	if len(params) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(params, ";") + "m"
}
