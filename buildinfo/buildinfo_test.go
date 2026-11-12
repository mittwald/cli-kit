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

// The version a build reports, from each way a binary is built.
func TestGetVersionByHowItWasBuilt(t *testing.T) {
	const (
		revision = "2f9134944bc2aa36e0b0ad7bb4b1e5b6a1d2c3e4"
		vcsTime  = "2026-09-29T16:04:45Z"
	)
	vcs := func(modified bool) []debug.BuildSetting {
		return []debug.BuildSetting{
			{Key: "vcs.revision", Value: revision},
			{Key: "vcs.time", Value: vcsTime},
			{Key: "vcs.modified", Value: map[bool]string{true: "true", false: "false"}[modified]},
		}
	}
	for _, tc := range []struct {
		name            string
		stamp, commit   string
		build           *debug.BuildInfo
		version, string string
		detail          string
	}{
		{
			name:  "release stamped by GoReleaser",
			stamp: "v1.2.3", commit: revision,
			build:   &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}, Settings: vcs(false)},
			version: "v1.2.3", string: "1.2.3",
			detail: "tool 1.2.3\ncommit: " + revision + "\n",
		},
		{
			name:    "go build of a clean checkout at a release tag",
			build:   &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}, Settings: vcs(false)},
			version: "v1.2.3", string: "1.2.3",
			detail: "tool 1.2.3\ncommit: " + revision + "\n",
		},
		{
			name:    "go build of a release tag with uncommitted changes",
			build:   &debug.BuildInfo{Main: debug.Module{Version: "v1.1.49+dirty"}, Settings: vcs(true)},
			version: DevVersion, string: "dev+2f9134944bc2.dirty",
			detail: "tool dev+2f9134944bc2.dirty\ncommit: " + revision + " (modified)\n",
		},
		{
			name: "go build of a commit after a tag",
			build: &debug.BuildInfo{
				Main:     debug.Module{Version: "v1.1.1-alpha.2.0.20260929160445-2f9134944bc2+dirty"},
				Settings: vcs(true),
			},
			version: DevVersion, string: "dev+2f9134944bc2.dirty",
		},
		{
			name:  "Makefile stamp dev with a short commit",
			stamp: "dev", commit: "2f9134944bc2",
			build: &debug.BuildInfo{
				Main:     debug.Module{Version: "v1.2.4-0.20260929160445-2f9134944bc2"},
				Settings: vcs(false),
			},
			version: DevVersion, string: "dev+2f9134944bc2",
			detail: "tool dev+2f9134944bc2\ncommit: 2f9134944bc2\n",
		},
		{
			name:    "go install module@v1.2.3",
			build:   &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}},
			version: "v1.2.3", string: "1.2.3",
			detail: "tool 1.2.3\n",
		},
		{
			name:    "go install module@v2.0.0+incompatible",
			build:   &debug.BuildInfo{Main: debug.Module{Version: "v2.0.0+incompatible"}},
			version: "v2.0.0+incompatible", string: "2.0.0+incompatible",
		},
		{
			name:    "go install module@main",
			build:   &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20260929160445-2f9134944bc2"}},
			version: DevVersion, string: "dev+2f9134944bc2",
			detail: "tool dev+2f9134944bc2\ncommit: 2f9134944bc2\nbuilt:  " + vcsTime + "\n",
		},
		{
			name:    "go build outside a repository",
			build:   &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			version: DevVersion, string: "dev",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := get(tc.stamp, tc.commit, "", readerFor(tc.build))
			if info.Version != tc.version || info.String() != tc.string {
				t.Errorf("Version = %q, String() = %q; want %q, %q", info.Version, info.String(), tc.version, tc.string)
			}
			if tc.detail != "" {
				if got := info.Detail("tool"); !strings.HasPrefix(got, tc.detail) {
					t.Errorf("Detail() = %q, want it to start with %q", got, tc.detail)
				}
			}
		})
	}
}

func TestPseudoVersion(t *testing.T) {
	for v, want := range map[string]string{
		"v0.0.0-20261110080000-0123456789ab":                "0123456789ab",
		"v1.2.4-0.20261110080000-0123456789ab":              "0123456789ab",
		"v1.2.4-rc.1.0.20261110080000-0123456789ab+dirty":   "0123456789ab",
		"v2.0.1-0.20261110080000-0123456789ab+incompatible": "0123456789ab",
		"v1.2.3":                             "",
		"v1.2.3-rc.1":                        "",
		"v1.2.3+dirty":                       "",
		"v1.2.3-20261110080000-0123456789ab": "",
		"(devel)":                            "",
		"v0.0.0-2026111008000-0123456789ab":  "",
	} {
		revision, committed, ok := pseudoVersion(v)
		if revision != want || ok != (want != "") {
			t.Errorf("pseudoVersion(%q) = %q, %v; want %q", v, revision, ok, want)
		}
		if ok && !committed.Equal(time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)) {
			t.Errorf("pseudoVersion(%q) time = %v", v, committed)
		}
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
