package tui

import "reflect"

// Widget is anything drawn on the screen. Draw is called for every frame,
// with a canvas of the size the layout gave the widget; it must not block.
//
// What else a widget does it says by the interfaces it implements:
// [Focusable] to take the focus and keys, [Container] to hold other
// widgets, [Bindings] to bind keys, [Paster] to take pasted text,
// [Clicker] to take the mouse and [Sizer] to tell layouts how big it wants
// to be.
type Widget interface {
	Draw(c Canvas)
}

// Focusable is a widget that can take the focus. Tab and Shift-Tab move the
// focus between the focusable widgets of a screen in the order a walk of
// the widget tree finds them.
type Focusable interface {
	Widget
	// CanFocus reports whether the widget takes the focus now: a disabled
	// field does not.
	CanFocus() bool
	// HandleKey is given a key the bindings of the widget did not take, and
	// reports whether it used it. A text field takes the characters typed
	// into it this way.
	HandleKey(k Key) bool
}

// Container is a widget that holds others. Children returns the ones shown
// now, in the order the focus moves through them.
type Container interface {
	Widget
	Children() []Widget
}

// Bindings is a widget with keys of its own. The keymap is asked while the
// widget or one inside it has the focus, before the widget's HandleKey, and
// its bindings show in the legend.
type Bindings interface {
	Keymap() *Keymap
}

// Paster is a widget that takes pasted text while it has the focus.
type Paster interface {
	HandlePaste(text string) bool
}

// Clicker is a widget that takes mouse events in the rectangle it was
// drawn into. X and Y of the event are relative to that rectangle.
type Clicker interface {
	HandleMouse(m Mouse) bool
}

// Sizer is a widget that tells layouts how big it wants to be in a space of
// width×height cells. A layout gives a child of size [Auto] what it asks
// for, if there is room.
type Sizer interface {
	Size(width, height int) (w, h int)
}

// Focuser is a widget that wants to know when it gains or loses the focus.
type Focuser interface {
	FocusChanged(focused bool)
}

// same reports whether a and b are the same widget. Widgets are told apart
// by ==, which suits the pointers widgets usually are; a widget of a type
// == cannot compare, such as a struct holding a slice, is the same as
// nothing else, rather than a panic.
func same(a, b any) (eq bool) {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ta := reflect.TypeOf(a)
	if ta != reflect.TypeOf(b) || !ta.Comparable() {
		return false
	}
	defer func() {
		if recover() != nil {
			eq = false
		}
	}()
	return a == b
}

// contains reports whether list holds w.
func contains[W any](list []W, w any) bool {
	for _, x := range list {
		if same(x, w) {
			return true
		}
	}
	return false
}

// focusables returns the widgets of the tree under root that take the focus
// now, in order.
func focusables(root Widget) []Focusable {
	var out []Focusable
	var walk func(w Widget)
	walk = func(w Widget) {
		if f, ok := w.(Focusable); ok && f.CanFocus() {
			out = append(out, f)
		}
		if c, ok := w.(Container); ok {
			for _, child := range c.Children() {
				if child != nil {
					walk(child)
				}
			}
		}
	}
	walk(root)
	return out
}

// pathTo returns the widgets from root down to target, both included, or
// nil when target is not in the tree.
func pathTo(root, target Widget) []Widget {
	if root == nil || target == nil {
		return nil
	}
	if same(root, target) {
		return []Widget{root}
	}
	c, ok := root.(Container)
	if !ok {
		return nil
	}
	for _, child := range c.Children() {
		if p := pathTo(child, target); p != nil {
			return append([]Widget{root}, p...)
		}
	}
	return nil
}

// Keyed wraps a widget and gives it keys of its own, such as the keys of a
// screen that apply whichever of its widgets has the focus.
type Keyed struct {
	child  Widget
	keymap *Keymap
}

// NewKeyed returns child with an empty keymap.
func NewKeyed(child Widget) *Keyed { return &Keyed{child: child, keymap: NewKeymap()} }

// Keymap returns the keys.
func (k *Keyed) Keymap() *Keymap { return k.keymap }

// Children returns the widget inside.
func (k *Keyed) Children() []Widget { return []Widget{k.child} }

// Draw draws the widget inside.
func (k *Keyed) Draw(c Canvas) { c.Draw(k.child) }

// Size is the size of the widget inside.
func (k *Keyed) Size(width, height int) (int, int) {
	if s, ok := k.child.(Sizer); ok {
		return s.Size(width, height)
	}
	return width, 1
}
