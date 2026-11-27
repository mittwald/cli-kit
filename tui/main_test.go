package tui

import (
	"os"
	"testing"
)

// TestMain draws every test in the dark themes, whatever background the
// terminal running the tests says it has; a test that wants the light ones
// sets COLORFGBG itself.
func TestMain(m *testing.M) {
	_ = os.Unsetenv("COLORFGBG")
	os.Exit(m.Run())
}
