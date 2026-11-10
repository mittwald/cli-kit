package buildinfo

import (
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func readerFor(build *debug.BuildInfo) func() (*debug.BuildInfo, bool) {
	return func() (*debug.BuildInfo, bool) { return build, build != nil }
}

func TestGetPrefersLinkerFlags(t *testing.T) {
	build := &debug.BuildInfo{
		GoVersion: "go1.24.9",
		Main:      debug.Module{Version: "v9.9.9"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "ffffffffffffffffffffffffffffffffffffffff"},
			{Key: "vcs.time", Value: "2020-01-01T00:00:00Z"},
		},
	}
	info := get("v1.2.3", "0123456789abcdef0123456789abcdef01234567", "2026-11-10T08:00:00Z", readerFor(build))
	if info.Version != "v1.2.3" {
		t.Errorf("Version = %q, want the stamped v1.2.3", info.Version)
	}
	if info.Commit != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("Commit = %q, want the stamped revision", info.Commit)
	}
	if want := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC); !info.Date.Equal(want) {
		t.Errorf("Date = %v, want %v", info.Date, want)
	}
	if info.GoVersion != "go1.24.9" {
		t.Errorf("GoVersion = %q, want go1.24.9", info.GoVersion)
	}
}

func TestGetFallsBackToToolchain(t *testing.T) {
	build := &debug.BuildInfo{
		Main: debug.Module{Version: "v1.5.0"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abcdefabcdefabcdefabcdefabcdefabcdefabcd"},
			{Key: "vcs.time", Value: "2026-01-02T03:04:05Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	}
	info := get("", "", "", readerFor(build))
	if info.Version != "v1.5.0" || info.Commit != "abcdefabcdefabcdefabcdefabcdefabcdefabcd" || !info.Modified {
		t.Fatalf("toolchain fallback not applied: %+v", info)
	}
	if want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC); !info.Date.Equal(want) {
		t.Errorf("Date = %v, want %v", info.Date, want)
	}
}

func TestGetDevelBuild(t *testing.T) {
	build := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}
	if info := get("", "", "", readerFor(build)); info.Version != DevVersion {
		t.Fatalf("Version = %q, want %q", info.Version, DevVersion)
	}
	if info := get("dev", "", "", readerFor(nil)); info.Version != DevVersion {
		t.Fatalf("Version without build info = %q, want %q", info.Version, DevVersion)
	}
}

func TestGetIgnoresUnparsableDate(t *testing.T) {
	if info := get("1.0.0", "", "yesterday", readerFor(nil)); !info.Date.IsZero() {
		t.Fatalf("Date = %v, want zero for an unparsable stamp", info.Date)
	}
}

func TestString(t *testing.T) {
	for _, tc := range []struct {
		info Info
		want string
	}{
		{Info{Version: "v1.4.0"}, "1.4.0"},
		{Info{Version: "V2.0.0-rc.1"}, "2.0.0-rc.1"},
		{Info{Version: "1.4.0"}, "1.4.0"},
		{Info{Version: "v0.0.0-20261110080000-0123456789ab"}, "0.0.0-20261110080000-0123456789ab"},
		{Info{Version: "version-one"}, "version-one"},
		{Info{Version: "v"}, "v"},
		{Info{}, "dev"},
		{Info{Version: "dev"}, "dev"},
		{Info{Version: "dev", Commit: "0123456789abcdef"}, "dev+0123456789ab"},
		{Info{Version: "dev", Commit: "0123abc", Modified: true}, "dev+0123abc.dirty"},
		{Info{Version: " v3.1.0 \n"}, "3.1.0"},
	} {
		if got := tc.info.String(); got != tc.want {
			t.Errorf("%+v.String() = %q, want %q", tc.info, got, tc.want)
		}
	}
}

func TestDetail(t *testing.T) {
	info := Info{
		Version:   "v1.2.3",
		Commit:    "0123456789abcdef",
		Modified:  true,
		Date:      time.Date(2026, 11, 10, 9, 0, 0, 0, time.FixedZone("CET", 3600)),
		GoVersion: "go1.24.9",
	}
	got := info.Detail("example")
	for _, want := range []string{
		"example 1.2.3\n",
		"commit: 0123456789abcdef (modified)\n",
		"built:  2026-11-10T08:00:00Z\n",
		"go:     go1.24.9\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Detail() = %q, missing %q", got, want)
		}
	}
	if got := (Info{Version: "1.0.0"}).Detail(""); got != "1.0.0\n" {
		t.Errorf("Detail without name or extras = %q", got)
	}
}
