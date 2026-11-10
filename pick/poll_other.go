//go:build !unix

package pick

import (
	"os"
	"time"
)

// readable has no portable way to wait on a console here, so it reports no
// input and a pending ESC resolves as the Esc key at once. A split arrow-key
// sequence can then be read as Esc, which terminals on these platforms do not
// produce in practice.
func readable(*os.File, time.Duration) bool { return false }
