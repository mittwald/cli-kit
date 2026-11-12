//go:build !unix

package pick

import (
	"os"
	"time"
)

// awaitInput has no portable way to wait on a console here, so it reports no
// input. A pending ESC then resolves as the Esc key at once: a split
// arrow-key sequence can be read as Esc, which terminals on these platforms
// do not produce in practice. And a read cannot be woken, so a prompt reads
// its keys with plain blocking reads here.
func awaitInput(*os.File, int, time.Duration) (ready, woken bool) { return false, false }
