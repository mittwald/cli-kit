package tui_test

import (
	"fmt"

	"github.com/mittwald/cli-kit/tui"
)

// A table of tickets with a filter and a legend, driven by keys in a
// headless run.
func Example() {
	type ticket struct{ id, title string }
	list := tui.NewList(func(t ticket) []tui.Span {
		return []tui.Span{{Text: t.id}, {Text: t.title}}
	}).SetColumns(tui.Column{Title: "ID"}, tui.Column{Title: "TITLE", Grow: 1})
	list.SetItems([]ticket{
		{"m-1", "Mail bounces"},
		{"m-2", "Backup restore"},
		{"m-3", "Mail quota"},
	})
	opened := ""
	list.OnSelect = func(t ticket) { opened = t.id }

	app := tui.New(tui.VBox(tui.Grow(list), tui.Fit(tui.NewLegend())))
	app.Keymap().Bind("quit", app.Quit, "q")

	// app.Run() would take the terminal; a headless run draws into memory.
	h := tui.NewHeadless(app, 40, 6)
	h.Press("/")
	h.Type("mail")
	h.Press("enter", "j", "enter")
	fmt.Println(h.Screen())
	fmt.Println("opened", opened)
	// Output:
	//   ID  TITLE
	//   m-1 Mail bounces
	//   m-3 Mail quota
	//
	// /mail
	// ↑/k move  ⏎ open  space mark  ? more
	// opened m-3
}

// A form in a dialog, with a field that must not stay empty.
func ExampleForm() {
	name := tui.NewInput()
	name.Validate = func(s string) error {
		if s == "" {
			return fmt.Errorf("a name is required")
		}
		return nil
	}
	name.SetValue("")
	notify := tui.NewSwitch("by email", true)
	form := tui.NewForm().Add("Name", name).Add("Notify", notify)
	form.OnSubmit = func() { fmt.Println("saved", name.Value(), notify.Value()) }

	h := tui.NewHeadless(tui.New(form), 30, 4)
	fmt.Println(h.Screen())
	h.Type("Ada")
	h.Press("tab", "space", "ctrl+s")
	// Output:
	//   Name
	//         a name is required
	//
	// Notify  by email  ON
	// saved Ada false
}
