package buildinfo_test

import (
	"fmt"
	"time"

	"github.com/mittwald/cli-kit/buildinfo"
)

func ExampleInfo_String() {
	release := buildinfo.Info{Version: "v1.4.0"}
	dev := buildinfo.Info{Version: buildinfo.DevVersion, Commit: "0123456789abcdef0123", Modified: true}
	fmt.Println(release)
	fmt.Println(dev)
	// Output:
	// 1.4.0
	// dev+0123456789ab.dirty
}

func ExampleInfo_Detail() {
	info := buildinfo.Info{
		Version:   "v1.4.0",
		Commit:    "0123456789abcdef0123456789abcdef01234567",
		Date:      time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC),
		GoVersion: "go1.24.9",
	}
	fmt.Print(info.Detail("example"))
	// Output:
	// example 1.4.0
	// commit: 0123456789abcdef0123456789abcdef01234567
	// built:  2026-11-10T08:00:00Z
	// go:     go1.24.9
}
