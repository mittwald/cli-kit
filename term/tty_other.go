//go:build !unix && !windows

package term

import "os"

// OpenTTY reports [ErrNoTTY]: this platform has no controlling terminal to
// open.
func OpenTTY() (*os.File, error) { return nil, ErrNoTTY }
