package notice

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/mittwald/cli-kit/redact"
	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// A message is put together from runs: stretches of its own text, and the
// values and pattern matches that carry a role.
type run struct {
	text  string
	role  Role
	value bool // has a role
}

// While the message is formatted, a [Value] is written as a mark: a
// private-use rune, a random nonce drawn for the notice, the value's
// index, and a closing private-use rune. Text from elsewhere cannot guess
// the nonce, so only these marks are read as values, and ordinary arguments
// go to fmt as they are, with every verb, width and precision fmt allows.
const (
	markOpen  = '\uE000'
	markClose = '\uE001'
	// markTab stands for a tab while the pieces are made plain one by one,
	// so the tab is expanded afterwards from where it stands in the whole
	// message, values included.
	markTab = '\uE002'
)

func newNonce() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// valueArg formats a [Value] as fmt would with the verb it meets, keeps
// that text, and writes a mark in its place.
type valueArg struct {
	v     value
	nonce string
	texts *[]run
}

func (a valueArg) Format(f fmt.State, verb rune) {
	i := len(*a.texts)
	*a.texts = append(*a.texts, run{text: fmt.Sprintf(fmt.FormatString(f, verb), a.v.v), role: a.v.role, value: true})
	_, _ = io.WriteString(f, string(markOpen)+a.nonce+strconv.Itoa(i)+string(markClose))
}

// runs formats the message and splits it into runs. Escape sequences are
// taken out of the message and of each value (term.Plain); credentials are
// looked for in the message as it reads, values in place, so a label in the
// format and a secret in a value are found together. Where redaction
// changes anything, the notice keeps no values or matches: it is the
// redacted text alone.
func (p *Printer) runs(format string, args []any) []run {
	var values []run
	nonce := newNonce()
	wrapped := make([]any, len(args))
	for i, arg := range args {
		if v, ok := arg.(value); ok {
			wrapped[i] = valueArg{v: v, nonce: nonce, texts: &values}
			continue
		}
		wrapped[i] = arg
	}
	text := fmt.Sprintf(format, wrapped...)
	plain := func(s string) string {
		if p.options.KeepEscapes {
			return s
		}
		return term.Plain(strings.ReplaceAll(s, "\t", string(markTab)))
	}
	text = plain(text)

	var pieces []run
	open := string(markOpen) + nonce
	for text != "" {
		start := strings.Index(text, open)
		if start < 0 {
			pieces = append(pieces, run{text: text})
			break
		}
		if start > 0 {
			pieces = append(pieces, run{text: text[:start]})
		}
		rest := text[start+len(open):]
		end := strings.IndexRune(rest, markClose)
		i, err := strconv.Atoi(rest[:max(end, 0)])
		if end < 0 || err != nil || i < 0 || i >= len(values) {
			pieces = append(pieces, run{text: text[start : start+len(open)]})
			text = rest
			continue
		}
		v := values[i]
		v.text = plain(v.text)
		pieces = append(pieces, v)
		text = rest[end+len(string(markClose)):]
	}

	expandTabs(pieces)
	if !p.options.NoRedact {
		full := plainText(pieces)
		if redacted := redact.Patterns(full); redacted != full {
			return p.patternRuns(redacted)
		}
	}
	var out []run
	for _, piece := range pieces {
		if piece.value {
			out = append(out, piece)
			continue
		}
		out = append(out, p.patternRuns(piece.text)...)
	}
	return out
}

// expandTabs replaces every markTab with the spaces up to the next tab
// stop, every 8 columns from the start of the message's line.
func expandTabs(pieces []run) {
	column := 0
	for i := range pieces {
		var b strings.Builder
		for line, text := range strings.Split(pieces[i].text, "\n") {
			if line > 0 {
				b.WriteByte('\n')
				column = 0
			}
			for part, segment := range strings.Split(text, string(markTab)) {
				if part > 0 {
					spaces := 8 - column%8
					b.WriteString(strings.Repeat(" ", spaces))
					column += spaces
				}
				b.WriteString(segment)
				column += term.VisibleWidth(segment)
			}
		}
		pieces[i].text = b.String()
	}
}

// patternRuns splits a message's own text by the theme's patterns, the
// first pattern to match a stretch of text taking it.
func (p *Printer) patternRuns(text string) []run {
	runs := []run{{text: text}}
	for _, pattern := range p.options.Theme.Patterns {
		if pattern.Match == nil {
			continue
		}
		var next []run
		for _, r := range runs {
			if r.value || r.text == "" {
				next = append(next, r)
				continue
			}
			last := 0
			for _, m := range pattern.Match.FindAllStringIndex(r.text, -1) {
				if m[0] == m[1] {
					continue
				}
				if m[0] > last {
					next = append(next, run{text: r.text[last:m[0]]})
				}
				next = append(next, run{text: r.text[m[0]:m[1]], role: pattern.Role, value: true})
				last = m[1]
			}
			if last < len(r.text) {
				next = append(next, run{text: r.text[last:]})
			}
		}
		runs = next
	}
	return runs
}

// render returns the notice as it is written, ending in a newline.
func (p *Printer) render(level Level, format string, args []any, colored bool) string {
	runs := p.runs(format, args)
	if !colored {
		return p.renderPlain(level, runs)
	}
	pal := p.palette()
	lc := pal.Levels[level]
	truecolor := term.TrueColor()
	st := func(s theme.Style) theme.Style { return adapt(s, truecolor) }
	t := p.options.Theme
	fill := !t.NoFill && (t.Layout == LayoutBar || t.Layout == LayoutBlock)

	lines := splitLines(runs)
	var b strings.Builder
	if t.Layout == LayoutLine {
		label := level.String()
		indent := strings.Repeat(" ", len(label)+2)
		for i, line := range lines {
			if level == LevelDebug {
				prefix := "+ "
				if i > 0 {
					prefix = "  "
				}
				b.WriteString(st(lc.Label).Paint(prefix + plainText(line)))
				b.WriteByte('\n')
				continue
			}
			if i == 0 {
				b.WriteString(st(lc.Label).Paint(label) + st(pal.Separator).Paint(":") + " ")
			} else {
				b.WriteString(indent)
			}
			p.writeRuns(&b, line, "", func(r Role) theme.Style { return st(p.termValue(pal, r)) })
			b.WriteByte('\n')
		}
		return b.String()
	}

	badgeText := strings.Repeat(" ", t.Padding.Left) + fmt.Sprintf("%-*s", labelWidth, level.String()) + strings.Repeat(" ", t.Padding.Right)
	blank := strings.Repeat(" ", len(badgeText))
	badge := st(theme.Join(theme.Bold, lc.Accent, lc.Text))
	for i, line := range lines {
		switch t.Layout {
		case LayoutBlock:
			base := st(theme.Join(lc.Accent, lc.Text))
			b.WriteString(base.Sequence())
			if i == 0 {
				b.WriteString(theme.Bold.Sequence() + badgeText + "\x1b[22m")
			} else {
				b.WriteString(blank)
			}
			b.WriteByte(' ')
			p.writeRuns(&b, line, base, func(r Role) theme.Style {
				return st(theme.Join(theme.Bold, p.accentValue(pal, level, r)))
			})
			b.WriteByte(' ')
		default: // LayoutBadge, LayoutBar
			if i == 0 {
				b.WriteString(badge.Paint(badgeText))
			} else {
				b.WriteString(blank)
			}
			var base theme.Style
			if t.Layout == LayoutBar {
				base = st(theme.Join(lc.Tint, lc.TintText))
			}
			b.WriteString(base.Sequence())
			b.WriteByte(' ')
			p.writeRuns(&b, line, base, func(r Role) theme.Style { return st(p.termValue(pal, r)) })
			if t.Layout == LayoutBar {
				b.WriteByte(' ')
			}
		}
		if fill {
			b.WriteString("\x1b[K")
		}
		if t.Layout != LayoutBadge {
			b.WriteString(theme.Reset)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// writeRuns writes a line's runs on base: own text in base, values in
// their style and base again after each.
func (p *Printer) writeRuns(b *strings.Builder, line []run, base theme.Style, style func(Role) theme.Style) {
	for _, r := range line {
		if !r.value {
			b.WriteString(r.text)
			continue
		}
		s := style(r.role)
		if s == "" {
			b.WriteString(r.text)
			continue
		}
		b.WriteString(s.Sequence() + r.text + theme.Reset + base.Sequence())
	}
}

// renderPlain is a notice without color: "LEVEL: message", or "+ message"
// for a debug notice in the line theme. Further lines are indented to the
// message.
func (p *Printer) renderPlain(level Level, runs []run) string {
	prefix := level.String() + ": "
	if level == LevelDebug && p.options.Theme.Layout == LayoutLine {
		prefix = "+ "
	}
	var b strings.Builder
	for i, line := range splitLines(runs) {
		if i == 0 {
			b.WriteString(prefix)
		} else {
			b.WriteString(strings.Repeat(" ", len(prefix)))
		}
		b.WriteString(plainText(line))
		b.WriteByte('\n')
	}
	return b.String()
}

// termValue is the color of a value on the terminal's own background.
func (p *Printer) termValue(pal Palette, r Role) theme.Style {
	if s := pal.Values[r]; s != "" {
		return s
	}
	if r.spec == nil {
		return ""
	}
	if p.light() {
		return r.spec.colors.Light
	}
	return r.spec.colors.Dark
}

// accentValue is the color of a value on a level's background.
func (p *Printer) accentValue(pal Palette, level Level, r Role) theme.Style {
	if s := pal.Levels[level].Values[r]; s != "" {
		return s
	}
	if r.spec != nil {
		return r.spec.colors.OnAccent[level]
	}
	return ""
}

// palette is the palette in use: the theme's colors and the options'
// colors over them, for the terminal's background.
func (p *Printer) palette() Palette {
	themeColors, over := p.options.Theme.Colors, p.options.Colors
	pal := themeColors.Dark
	if p.light() {
		pal = pal.merge(themeColors.Light)
	}
	pal = pal.merge(over.Dark)
	if p.light() {
		pal = pal.merge(over.Light)
	}
	return pal
}

// splitLines splits runs at newlines, a run that holds one becoming a run
// on each line.
func splitLines(runs []run) [][]run {
	lines := [][]run{nil}
	for _, r := range runs {
		parts := strings.Split(r.text, "\n")
		for i, part := range parts {
			if i > 0 {
				lines = append(lines, nil)
			}
			if part != "" {
				piece := r
				piece.text = part
				lines[len(lines)-1] = append(lines[len(lines)-1], piece)
			}
		}
	}
	return lines
}

func plainText(line []run) string {
	var b strings.Builder
	for _, r := range line {
		b.WriteString(r.text)
	}
	return b.String()
}

// adapt rewrites the 24-bit colors of s to the nearest of the 256 colors
// where the terminal does not take 24-bit colors, as term.TrueColor tells.
func adapt(s theme.Style, truecolor bool) theme.Style {
	if truecolor || !strings.Contains(string(s), ";2;") {
		return s
	}
	fields := strings.Split(string(s), ";")
	out := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		if (fields[i] == "38" || fields[i] == "48") && i+4 < len(fields) && fields[i+1] == "2" {
			r, errR := strconv.ParseUint(fields[i+2], 10, 8)
			g, errG := strconv.ParseUint(fields[i+3], 10, 8)
			b, errB := strconv.ParseUint(fields[i+4], 10, 8)
			if errR == nil && errG == nil && errB == nil {
				out = append(out, fields[i], "5", strconv.Itoa(term.Nearest256(uint8(r), uint8(g), uint8(b))))
				i += 4
				continue
			}
		}
		out = append(out, fields[i])
	}
	return theme.Style(strings.Join(out, ";"))
}
