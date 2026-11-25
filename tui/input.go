package tui

import (
	"strings"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// Input is a one-line text field. It edits by grapheme cluster, so an
// emoji or a letter with an accent is one character, and knows the keys of
// a shell's line editor:
//
//	←/→ ctrl+b/f          character left/right
//	alt+←/→ ctrl+←/→      word left/right (also alt+b/f)
//	home/end ctrl+a/e     start/end
//	backspace delete      delete a character (also ctrl+h/d)
//	ctrl+w alt+backspace  delete the word before the cursor
//	alt+d                 delete the word after the cursor
//	ctrl+u ctrl+k         delete to the start/end
//	ctrl+z ctrl+y         undo/redo
//	enter                 submit, where OnSubmit is set
//
// With suggestions set, the rest of the first suggestion that starts with
// what was typed shows after the cursor; → or End at the end of the text
// takes it, ctrl+n and ctrl+p go through the others.
type Input struct {
	ed          *editor
	placeholder string
	secret      bool
	disabled    bool
	first       int
	keymap      *Keymap
	suggest     func(text string) []string
	suggestions []string
	pick        int

	// OnChange is called with the text after every change.
	OnChange func(text string)
	// OnSubmit is called with the text when Enter is pressed.
	OnSubmit func(text string)
	// Validate checks the text after every change; the error it returns is
	// what Err reports, and what a Form shows under the field.
	Validate func(text string) error
}

// NewInput returns an empty text field.
func NewInput() *Input {
	in := &Input{ed: newEditor(false), keymap: NewKeymap()}
	in.keymap.Bind("submit", func() { in.OnSubmit(in.Value()) }, "enter").
		When(func() bool { return in.OnSubmit != nil }).Hide()
	in.keymap.Bind("next suggestion", func() { in.cycle(1) }, "ctrl+n").
		When(func() bool { return len(in.suggestions) > 1 }).InGroup("Text")
	in.keymap.Bind("previous suggestion", func() { in.cycle(-1) }, "ctrl+p").
		When(func() bool { return len(in.suggestions) > 1 }).Hide().InGroup("Text")
	return in
}

// Keymap returns the keys of the field.
func (in *Input) Keymap() *Keymap { return in.keymap }

// Value returns the text.
func (in *Input) Value() string { return in.ed.String() }

// SetValue replaces the text and puts the cursor at its end. OnChange is
// not called.
func (in *Input) SetValue(s string) {
	in.ed.set(oneLine(s))
	in.check()
}

// SetPlaceholder sets the text shown, muted, while the field is empty.
func (in *Input) SetPlaceholder(s string) *Input { in.placeholder = term.PlainLine(s); return in }

// SetSecret shows every character as a bullet, for a password or a token.
func (in *Input) SetSecret(secret bool) *Input { in.secret = secret; return in }

// SetLimit sets the most characters the field takes, 0 for no limit.
func (in *Input) SetLimit(n int) *Input { in.ed.limit = max(n, 0); return in }

// SetDisabled keeps the field from taking the focus.
func (in *Input) SetDisabled(disabled bool) *Input { in.disabled = disabled; return in }

// SetSuggest sets where suggestions for the text come from; nil for none.
func (in *Input) SetSuggest(fn func(text string) []string) *Input {
	in.suggest = fn
	in.refresh()
	return in
}

// Err returns what Validate said about the text, nil when it is fine.
func (in *Input) Err() error {
	if in.Validate != nil {
		return in.Validate(in.Value())
	}
	return nil
}

// CanFocus reports whether the field is enabled.
func (in *Input) CanFocus() bool { return !in.disabled }

// TakesText reports that the field takes typed characters.
func (in *Input) TakesText() bool { return true }

func (in *Input) check() {
	in.refresh()
}

func (in *Input) changed() {
	in.check()
	if in.OnChange != nil {
		in.OnChange(in.Value())
	}
}

// refresh takes the suggestions that complete the text.
func (in *Input) refresh() {
	in.suggestions, in.pick = nil, 0
	if in.suggest == nil || in.Value() == "" {
		return
	}
	v := in.Value()
	for _, s := range in.suggest(v) {
		if rest, ok := completes(term.PlainLine(s), v); ok {
			in.suggestions = append(in.suggestions, rest)
		}
	}
}

// completes returns the rest of s after the part that matches v, ignoring
// case, and whether s starts with v and goes on after it. Case folding can
// change the length of a character, so the end of the match is looked for
// in s itself.
func completes(s, v string) (string, bool) {
	for i := range s {
		if i > 0 && strings.EqualFold(s[:i], v) {
			return s[i:], true
		}
	}
	return "", false
}

func (in *Input) cycle(delta int) {
	n := len(in.suggestions)
	in.pick = ((in.pick+delta)%n + n) % n
}

// ghost is the rest of the suggestion shown after the text.
func (in *Input) ghost() string {
	if len(in.suggestions) == 0 || in.ed.col != len(in.ed.lines[0]) {
		return ""
	}
	return in.suggestions[in.pick]
}

// HandleKey edits the text.
func (in *Input) HandleKey(k Key) bool {
	if g := in.ghost(); g != "" && (k.Code == KeyRight || k.Code == KeyEnd) && k.Mod == 0 {
		in.ed.remember("")
		in.ed.insertText(g)
		in.changed()
		return true
	}
	took, changed := in.ed.handle(k)
	if changed {
		in.changed()
	}
	return took
}

// HandlePaste inserts pasted text, its line breaks as spaces.
func (in *Input) HandlePaste(text string) bool {
	in.ed.remember("")
	if in.ed.insertText(text) {
		in.changed()
	}
	return true
}

// HandleMouse puts the cursor where the field was clicked.
func (in *Input) HandleMouse(m Mouse) bool {
	if m.Button != MouseLeft || m.Action != MousePress {
		return false
	}
	line := in.ed.lines[0]
	in.first = min(in.first, len(line))
	// Typing after a click is an edit of its own for undo.
	in.ed.lastGroup = ""
	if in.secret {
		// Every character shows as one bullet.
		in.ed.col = in.first + min(max(m.X, 0), len(line)-in.first)
		return true
	}
	in.ed.col = colAt(line[in.first:], m.X) + in.first
	return true
}

// Draw draws the field across its width.
func (in *Input) Draw(c Canvas) {
	th := c.Theme()
	focused := c.Focused(in)
	style := th.Field
	if focused {
		style = th.FieldFocused
	}
	c.FillRow(0, 0, style)
	line := in.ed.lines[0]
	in.first = min(in.first, len(line))
	if len(line) == 0 && in.placeholder != "" {
		c.Text(0, 0, in.placeholder, theme.Join(style, th.Muted))
		if focused {
			c.ShowCursor(0, 0)
		}
		return
	}
	shown := line
	if in.secret {
		shown = make([]string, len(line))
		for i := range shown {
			shown[i] = "•"
		}
	}
	// Scroll so the cursor stays in view, with a cell for it at the end.
	room := max(c.Width()-1, 1)
	col := in.ed.col
	if col < in.first {
		in.first = col
	}
	// The width before the cursor is measured once and shrinks with every
	// cluster scrolled out, so a long value does not take quadratic time.
	for dist := colWidth(shown[in.first:], col-in.first); dist > room; in.first++ {
		dist -= clusterWidth(shown[in.first])
	}
	x := 0
	for _, cl := range shown[in.first:] {
		if x+clusterWidth(cl) > c.Width() {
			break
		}
		x += c.Text(x, 0, cl, style)
	}
	if g := in.ghost(); g != "" && !in.secret {
		c.Text(x, 0, g, theme.Join(style, th.Muted))
	}
	if focused {
		c.ShowCursor(colWidth(shown[in.first:], col-in.first), 0)
	}
}

// Size is the width it is given and one line.
func (in *Input) Size(width, _ int) (int, int) { return width, 1 }
