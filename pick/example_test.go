package pick_test

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mittwald/cli-kit/pick"
	"github.com/mittwald/cli-kit/theme"
)

func Example() {
	index, err := pick.Select(os.Stdin, os.Stderr, pick.Options{
		Title:   "Which version of %s?",
		Subject: "tool",
		Items: []pick.Item{
			{Cells: []string{"1.4.2", "latest"}, Action: "install"},
			{Cells: []string{"1.3.9", "installed ✓"}, Disabled: true},
			{Cells: []string{"1.2.0 (legacy)"}, Action: "install", Muted: true},
		},
	})
	switch {
	case errors.Is(err, pick.ErrNotATerminal):
		fmt.Println("pass --version instead")
	case errors.Is(err, pick.ErrAborted):
		fmt.Println("nothing selected")
	case err == nil:
		fmt.Println("chose row", index)
	}
}

func ExampleSelectOrType() {
	versions := []pick.Item{{Cells: []string{"1.4.2"}}, {Cells: []string{"1.4.1"}}}
	index, typed, err := pick.SelectOrType(os.Stdin, os.Stderr, pick.Options{
		Title:     "Release which version?",
		Items:     versions,
		Typed:     func(text string) bool { return strings.Count(text, ".") == 2 },
		TypedNote: "new version",
	})
	if err != nil {
		return
	}
	if index < 0 {
		fmt.Println("new version", typed)
	}
}

func ExampleSelectView() {
	view, index, err := pick.SelectView(os.Stdin, os.Stderr, pick.Options{
		Title: "Pick a branch",
		Views: []pick.View{
			{Name: "local", Items: []pick.Item{{Cells: []string{"main"}, Key: "main"}}},
			{Name: "remote", Loading: "fetching remote branches…", Load: func() ([]pick.Item, error) {
				return []pick.Item{{Cells: []string{"origin/main"}, Key: "main"}}, nil
			}},
		},
		ColumnPriority:   []int{pick.PriorityHigh},
		ColumnTruncation: []pick.Truncation{pick.TruncateMiddle},
	})
	if err == nil {
		fmt.Println(view, index)
	}
}

func ExampleSelectRange() {
	first, last, err := pick.SelectRange(os.Stdin, os.Stderr, pick.Options{
		Title: "Which changes?",
		Items: []pick.Item{
			{Cells: []string{"c3", "newest"}, Detail: "c3"},
			{Cells: []string{"b2", "middle"}, Detail: "b2"},
			{Cells: []string{"a1", "oldest"}, Detail: "a1"},
		},
		Range:      true,
		RangeUnit:  "change",
		CellStyles: []theme.Style{theme.Fg256(71)},
	})
	if err == nil {
		fmt.Println(first, last)
	}
}

func ExampleInput() {
	password, err := pick.Input(os.Stdin, os.Stderr, pick.InputOptions{
		Prompt:  "Password for %s: ",
		Subject: "alice@example.com",
		Secret:  true,
	})
	if err == nil && password != "" {
		fmt.Println("got a password")
	}
}

func ExampleToggles() {
	values, err := pick.Toggles(os.Stdin, os.Stderr, pick.ToggleOptions{
		Title:   "Settings for %s",
		Subject: "service-a",
		Toggles: []pick.Toggle{
			{Label: "Enabled", On: "enabled", Off: "disabled", Value: true},
			{Label: "Public", On: "public", Off: "private"},
		},
	})
	if err == nil {
		fmt.Println(values)
	}
}

func ExampleChecklist() {
	ticked, err := pick.Checklist(os.Stdin, os.Stderr, pick.ChecklistOptions{
		Title: "Which tokens should be created?",
		Boxes: []pick.Box{
			{Label: "feed", Note: "calendar feed", Checked: true},
			{Label: "smtp", Note: "send mail"},
		},
	})
	if err == nil {
		fmt.Println(ticked)
	}
}

func ExamplePrompt() {
	// Off a terminal the prompt is plain text with the subject filled in.
	fmt.Println(pick.Prompt(os.Stdout, "Recreate %s? [y/N]: ", "tool-1.2") + "y")
	// Output: Recreate tool-1.2? [y/N]: y
}

func ExampleStatus() {
	done := pick.Status(os.Stdout, "Fetching releases...")
	// ... fetch ...
	done() // erases the line on a terminal; keeps it anywhere else
	// Output: Fetching releases...
}

func ExampleName() {
	fmt.Println("Installing", pick.Name(os.Stdout, "tool (stable)"))
	// Output: Installing tool (stable)
}
