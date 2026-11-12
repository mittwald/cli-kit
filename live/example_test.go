package live_test

import (
	"bytes"
	"fmt"
	"os"

	"github.com/mittwald/cli-kit/live"
)

func ExampleBlocks() {
	blocks := live.NewBlocks(os.Stdout)
	fmt.Fprintln(blocks.Plain(), "Building release 1.4.2")
	fmt.Fprintln(blocks.Detail(), "compiling 12 packages")
	fmt.Fprintln(blocks.Detail(), "linking binary")
	fmt.Fprintln(blocks.Plain(), "Built release 1.4.2")
	// Output:
	// Building release 1.4.2
	//
	// compiling 12 packages
	// linking binary
	//
	// Built release 1.4.2
}

func ExampleLive() {
	var screen bytes.Buffer
	block := live.New(&screen)
	_ = block.Draw([]string{"downloading: 10%"})
	_ = block.Draw([]string{"downloading: 60%"})
	fmt.Printf("%q\n", screen.String())
	// Output:
	// "\x1b[Jdownloading: 10%\n\r\x1b[1A\x1b[Jdownloading: 60%\n"
}

func ExampleCursor() {
	// Show the cursor again if the program is interrupted while it is hidden.
	defer live.InstallSignalRestore()()

	cursor := live.NewCursor(os.Stderr)
	cursor.Hide()
	defer cursor.Show()

	go func() {
		defer cursor.RestoreOnPanic()
		cursor.Lock()
		defer cursor.Unlock()
		fmt.Fprint(os.Stderr, "\rworking…")
	}()
}

func ExampleOnRestore() {
	// A prompt that put the terminal into raw mode registers how to put it
	// back, for the exits that skip its deferred calls.
	remove := live.OnRestore(func() { fmt.Println("terminal mode restored") })
	defer remove()

	// The signal handler of InstallSignalRestore does this before it ends
	// the process.
	live.RestoreAll()
	// Output:
	// terminal mode restored
}
