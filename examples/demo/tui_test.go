package main

import (
	"strings"
	"testing"

	"github.com/mittwald/cli-kit/tui"
)

// TestTicketDemo walks through the screens of the tui demo.
func TestTicketDemo(t *testing.T) {
	h := tui.NewHeadless(newTicketApp(), 110, 26)
	want := func(what string, parts ...string) {
		t.Helper()
		s := h.Screen()
		for _, p := range parts {
			if !strings.Contains(s, p) {
				t.Fatalf("%s: screen lacks %q:\n%s", what, p, s)
			}
		}
	}
	want("list", "Tickets", "STATUS", "Serverperformance", "f filters")
	h.Press("/")
	h.Type("versand")
	h.Press("enter")
	want("filtered", "/versand", "1/5")
	h.Press("esc", "f")
	want("filter dialog", "Filters", "Department", "  Apply  ")
	h.Press("esc", "m")
	want("menu", "Assign to me", "Close ticket")
	h.Press("esc", "enter")
	want("ticket", "Write a reply", "Details", "internal note", "0/8000")
	h.Press("r")
	h.Type("Hallo")
	want("reply", "Hallo", "5/8000", "^s send")
	h.Press("ctrl+t")
	want("public", "visible to the customer")
	h.Press("ctrl+s")
	want("confirm", "Send to the customer")
	h.Press("n", "esc")
	want("back", "STATUS")
}

func TestClosingATicketTakesItOffTheOpenList(t *testing.T) {
	app := newTicketApp()
	h := tui.NewHeadless(app, 110, 26)
	h.Press("m", "c", "enter")
	lines := h.Lines()
	// The last line is the status bar, which names the ticket closed.
	if strings.Contains(strings.Join(lines[:len(lines)-1], "\n"), "m-3290102") || !strings.Contains(lines[len(lines)-1], "OK") {
		t.Errorf("a closed ticket stays listed:\n%s", h.Screen())
	}
}
