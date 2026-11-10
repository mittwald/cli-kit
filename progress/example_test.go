package progress_test

import (
	"log"
	"os"
	"time"

	"github.com/mittwald/cli-kit/progress"
)

func Example() {
	bar := progress.New(os.Stderr, progress.Options{})
	logger := log.New(bar.Writer(os.Stderr), "", 0)

	steps := []string{"Resolving dependencies", "Downloading", "Installing"}
	bar.Start(len(steps), 30*time.Second)
	for _, step := range steps {
		bar.Advance(step)
		done := bar.BeginWait() // for example around an HTTP request
		logger.Printf("%s: ok", step)
		done()
	}
	bar.Finish(true)
	// Erase the bar right before writing the result.
	bar.Clear()
}
