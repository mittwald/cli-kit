package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mittwald/cli-kit/pick"
	"github.com/mittwald/cli-kit/theme"
)

// newPickCommand returns the pick command: a list to choose a release from,
// then a typed answer and a hidden one.
func newPickCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "pick",
		Short: "Choose from a list, then type a text and a secret",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runPick()
		},
	}
}

func runPick() error {
	releases := []pick.Item{
		{Cells: []string{"1.4.2", "latest", "2 days ago"}, Action: "deploy", Groups: []string{"stable"}},
		{Cells: []string{"1.4.1", "current", "2 weeks ago"}, Action: "redeploy", Groups: []string{"stable"}},
		{Cells: []string{"1.5.0-rc.1", "release candidate", "yesterday"}, Action: "try", Groups: []string{"preview"}},
		{Cells: []string{"1.3.9", "end of life", "4 months ago"}, Muted: true, Groups: []string{"stable"}},
		{Cells: []string{"1.2.0", "withdrawn"}, Disabled: true, Groups: []string{"stable"}},
	}
	// Views narrow the rows to their groups; Tab switches between them.
	_, index, err := pick.SelectView(os.Stdin, os.Stderr, pick.Options{
		Title:   "Deploy which version of %s?",
		Subject: "api",
		Note:    "Muted rows are old, the withdrawn one cannot be chosen.",
		Items:   releases,
		Views: []pick.View{
			{Name: "all"},
			{Name: "stable", Groups: []string{"stable"}},
			{Name: "preview", Groups: []string{"preview"}},
		},
	})
	switch {
	case errors.Is(err, pick.ErrNotATerminal):
		fmt.Println("pick needs a terminal: run it without a pipe or redirection.")
		return nil
	case errors.Is(err, pick.ErrAborted):
		fmt.Println("Nothing chosen.")
		return nil
	case err != nil:
		return err
	}
	th := theme.Active().For(os.Stdout)
	fmt.Println("Chose", th.Value.Paint(releases[index].Cells[0]))

	note, err := pick.Input(os.Stdin, os.Stderr, pick.InputOptions{
		Prompt: "Note for the deployment of %s: ",
		// What a user typed is shown as plain text.
		Subject: releases[index].Cells[0],
		Value:   "routine update",
	})
	if err != nil {
		return ignoreAbort(err)
	}
	token, err := pick.Input(os.Stdin, os.Stderr, pick.InputOptions{Prompt: "Token (hidden): ", Secret: true})
	if err != nil {
		return ignoreAbort(err)
	}
	fmt.Printf("Note %q, token of %d characters %s\n",
		note, len([]rune(token)), th.Muted.Paint("("+strings.Repeat("•", min(len([]rune(token)), 8))+")"))
	return nil
}

// ignoreAbort turns Esc or Ctrl-C at a prompt into a quiet exit.
func ignoreAbort(err error) error {
	if errors.Is(err, pick.ErrAborted) {
		fmt.Println("Canceled.")
		return nil
	}
	return err
}
