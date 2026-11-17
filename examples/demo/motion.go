package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/mittwald/cli-kit/live"
	"github.com/mittwald/cli-kit/progress"
	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// newProgressCommand returns the progress command: a bar over fake steps,
// one of which stalls long enough to show the countdown.
func newProgressCommand() *cobra.Command {
	var stall bool
	cmd := &cobra.Command{
		Use:   "progress",
		Short: "Show a progress bar with log lines and a stalled step",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			runProgress(stall)
			return nil
		},
	}
	cmd.Flags().BoolVar(&stall, "stall", true, "let one step stall to show the countdown")
	return cmd
}

func runProgress(stall bool) {
	type step struct {
		name string
		took time.Duration
	}
	steps := []step{
		{"Resolving dependencies", 300 * time.Millisecond},
		{"Downloading archive", 600 * time.Millisecond},
		{"Verifying checksum", 200 * time.Millisecond},
		{"Waiting for the registry", 2500 * time.Millisecond},
		{"Installing", 400 * time.Millisecond},
	}
	if !stall {
		steps[3].took = 300 * time.Millisecond
	}

	// The bar counts down after a second of waiting, sooner than the default,
	// so the demo shows it quickly.
	bar := progress.New(os.Stderr, progress.Options{StallDelay: time.Second})
	// Log lines written through the bar appear above it, never under it.
	logger := log.New(bar.Writer(os.Stderr), "", 0)
	bar.Start(len(steps), 10*time.Second)
	for _, s := range steps {
		bar.Advance(s.name)
		done := bar.BeginWait()
		time.Sleep(s.took)
		done()
		logger.Printf("%s: done in %s", s.name, s.took)
	}
	bar.Finish(true)
	// Erase the bar right before the result.
	bar.Clear()
	fmt.Println(theme.Active().For(os.Stdout).Success.Paint("Installed 1.4.2"))
}

// newLiveCommand returns the live command: a block of status lines redrawn
// in place.
func newLiveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "live",
		Short: "Redraw a block of lines in place",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runLive()
		},
	}
}

func runLive() error {
	th := theme.Active().For(os.Stdout)
	jobs := []string{"build", "test", "lint", "package"}
	progress := make([]int, len(jobs))
	render := func() []string {
		lines := make([]string, len(jobs))
		for i, job := range jobs {
			state := th.Muted.Paint(fmt.Sprintf("%3d%%", progress[i]))
			if progress[i] == 100 {
				state = th.Success.Paint("done")
			}
			lines[i] = fmt.Sprintf("%-8s %s", job, state)
		}
		return lines
	}

	// Where stdout cannot be redrawn in place (a pipe, a file, TERM=dumb),
	// show the result once instead of every frame.
	if !term.CanAnimate(os.Stdout) {
		for i := range progress {
			progress[i] = 100
		}
		for _, line := range render() {
			fmt.Println(line)
		}
		return nil
	}

	cursor := live.NewCursor(os.Stdout)
	cursor.Hide()
	defer cursor.Show()
	block := live.New(os.Stdout)
	for tick := 0; ; tick++ {
		finished := true
		for i := range progress {
			// Each job moves at its own pace.
			progress[i] = min(100, tick*(i+2)*3)
			finished = finished && progress[i] == 100
		}
		if err := block.Draw(render()); err != nil {
			return err
		}
		if finished {
			return nil
		}
		time.Sleep(60 * time.Millisecond)
	}
}
