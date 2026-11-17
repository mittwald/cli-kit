package pick

import (
	"slices"
	"strconv"
)

// plainViews copies the views with what they draw made plain text. The copy
// is the list's own, so rows a view loads never show up in the caller's.
func plainViews(views []View) []View {
	plain := slices.Clone(views)
	for i := range plain {
		plain[i].Name = plainLine(plain[i].Name)
		plain[i].Title = plainLine(plain[i].Title)
		plain[i].Loading = plainLine(plain[i].Loading)
		plain[i].Empty = plainLine(plain[i].Empty)
	}
	return plain
}

// viewLabel names a view in the status line and in a Lens: its name, or its
// position, counted from 1, when it has none.
func (l *list) viewLabel(view int) string {
	return firstNonEmpty(l.views[view].Name, strconv.Itoa(view+1))
}

// switchable reports whether there are views to switch between.
func (l *list) switchable() bool { return len(l.views) > 1 }

// switchView moves delta views along, wrapping at both ends. It does nothing
// while a range is open or when there is no view to switch to.
func (l *list) switchView(delta int) {
	if !l.switchable() || l.anchor >= 0 {
		return
	}
	n := len(l.views)
	if !l.scoped {
		l.view = (l.view + delta + n) % n
		l.applyViewTitle()
		l.setFilter(l.filter)
		return
	}
	key := ""
	if index := l.selected(); index >= 0 && index < len(l.items) {
		key = l.items[index].Key
	}
	l.view = (l.view + delta + n) % n
	// Initial was about the view the list opened on.
	l.initial = 0
	l.pendingKey = ""
	if l.loading() {
		l.pendingKey = key
	}
	l.loadView(key)
}

// applyViewTitle shows the current view's title, or the options' title for
// a view without one.
func (l *list) applyViewTitle() {
	if len(l.views) > 0 {
		l.title = firstNonEmpty(l.views[l.view].Title, l.fallback)
	}
}

// loadView makes the current view's own rows the ones on screen. The filter
// carries over, and the cursor lands on the row carrying key, or at the top.
func (l *list) loadView(key string) {
	view := l.views[l.view]
	l.applyViewTitle()
	l.setItems(view.Items)
	l.cursor, l.top = 0, 0
	l.applyFilter()
	// The view the list opened on starts where Options.Initial says, also
	// when its rows only arrive now.
	if !l.loading() {
		l.moveToItem(l.initial)
		l.initial = 0
	}
	if key != "" {
		for position, index := range l.matches {
			if l.items[index].Key == key {
				l.cursor = position
				break
			}
		}
	}
	l.scrollToCursor()
}

// needsLoad reports whether the current view has rows to fetch and nobody
// fetching them yet.
func (l *list) needsLoad() bool {
	return l.scoped && l.views[l.view].Load != nil && l.loads[l.view] == loadWaiting
}

// loading reports whether the current view is still without the rows it
// loads: the fetch has not started yet, or has not come back.
func (l *list) loading() bool {
	return l.scoped && l.views[l.view].Load != nil && l.loads[l.view] != loadDone
}

// nextLoad hands the key loop the fetch of the current view's rows, once:
// a function that calls the view's Load, to be run off the key loop, whose
// result takes the rows into the list, back on the key loop. The key loop
// asks after a draw, so the view and its loading line are on screen while
// the fetch runs, and the list keeps taking keys meanwhile: Esc leaves it,
// Tab moves on to another view, and typing already filters what is to come.
func (l *list) nextLoad() func() func() {
	if !l.needsLoad() {
		return nil
	}
	view := l.view
	load := l.views[view].Load
	l.loads[view] = loadRunning
	return func() func() {
		items, err := load()
		return func() { l.finishLoad(view, items, err) }
	}
}

// finishLoad takes the rows a view loaded. They go on screen when the list
// still stands on that view, with the cursor on the row it was to land on,
// and are kept for the view otherwise.
func (l *list) finishLoad(view int, items []Item, err error) {
	l.loads[view] = loadDone
	if err != nil {
		// Whatever was gathered before the failure is still worth showing;
		// with nothing gathered, the rows the view already had stay.
		l.failures[view] = plainLine(err.Error())
		if len(items) == 0 {
			return
		}
	}
	l.views[view].Items = items
	if view != l.view {
		return
	}
	key := l.pendingKey
	l.pendingKey = ""
	l.loadView(key)
}
