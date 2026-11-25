package tui

import (
	"slices"
	"strings"
)

// Binding binds keys to an action.
type Binding struct {
	// Keys are the names of the keys, as [Key.String] names them: "j",
	// "down", "ctrl+s", "shift+tab", "?".
	Keys []string
	// Help says what the keys do, in a word or two: "open", "move".
	Help string
	// Group is the heading the binding is listed under in the help.
	Group string
	// Hidden keeps the binding out of the legend; the help still lists it.
	Hidden bool
	// Enabled, when set, reports whether the binding applies now. A binding
	// that does not apply neither takes its keys nor shows.
	Enabled func() bool
	// Action is what the keys do.
	Action func()
}

func (b *Binding) active() bool { return b.Enabled == nil || b.Enabled() }

// Hide keeps the binding out of the legend and returns it.
func (b *Binding) Hide() *Binding { b.Hidden = true; return b }

// InGroup sets the heading of the binding in the help and returns it.
func (b *Binding) InGroup(group string) *Binding { b.Group = group; return b }

// When makes the binding apply only while enabled reports true, and returns
// it.
func (b *Binding) When(enabled func() bool) *Binding { b.Enabled = enabled; return b }

// Label is how the legend shows the keys: the short forms of the first two,
// joined by "/".
func (b *Binding) Label() string {
	keys := b.Keys
	if len(keys) > 2 {
		keys = keys[:2]
	}
	labels := make([]string, len(keys))
	for i, k := range keys {
		labels[i] = KeyLabel(k)
	}
	return strings.Join(labels, "/")
}

// Keymap is an ordered set of bindings. The first binding of a key wins.
type Keymap struct {
	bindings []*Binding
}

// NewKeymap returns an empty keymap.
func NewKeymap() *Keymap { return &Keymap{} }

// Bind binds keys to action, with help for the legend, and returns the
// binding to be refined further:
//
//	km.Bind("open", open, "enter")
//	km.Bind("refresh", reload, "r", "ctrl+r").InGroup("List")
func (m *Keymap) Bind(help string, action func(), keys ...string) *Binding {
	b := &Binding{Keys: keys, Help: help, Action: action}
	m.bindings = append(m.bindings, b)
	return b
}

// Add adds bindings to the keymap.
func (m *Keymap) Add(bindings ...*Binding) {
	m.bindings = append(m.bindings, bindings...)
}

// Unbind removes every binding of key.
func (m *Keymap) Unbind(key string) {
	kept := m.bindings[:0]
	for _, b := range m.bindings {
		var keys []string
		for _, k := range b.Keys {
			if k != key {
				keys = append(keys, k)
			}
		}
		if len(keys) > 0 {
			b.Keys = keys
			kept = append(kept, b)
		}
	}
	m.bindings = kept
}

// Lookup returns the binding that applies to k, or nil.
func (m *Keymap) Lookup(k Key) *Binding {
	if m == nil {
		return nil
	}
	name := k.String()
	for _, b := range m.bindings {
		if !b.active() {
			continue
		}
		if slices.Contains(b.Keys, name) {
			return b
		}
	}
	return nil
}

// Bindings returns the bindings that apply now, in order.
func (m *Keymap) Bindings() []*Binding {
	if m == nil {
		return nil
	}
	var out []*Binding
	for _, b := range m.bindings {
		if b.active() {
			out = append(out, b)
		}
	}
	return out
}
