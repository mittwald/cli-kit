//go:build windows

package live

import (
	"io"

	"github.com/mittwald/cli-kit/term"
)

// console stands for the one console a Windows process is attached to.
type console struct{}

// terminalID returns the console for every writer that is one, and false
// for any other writer.
func terminalID(w io.Writer) (any, bool) {
	if !term.IsTerminal(w) {
		return nil, false
	}
	return console{}, true
}

// foreground reports true: Windows has no job control that would put the
// process into the background of its console.
func foreground(io.Writer) bool { return true }
