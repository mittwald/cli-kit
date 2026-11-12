package selfupdate

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v1.2.3", "1.2.3", 0},
		{"1.2", "1.2.0", 0},
		{"v1.2.4", "v1.2.3", 1},
		{"v1.10.0", "v1.9.9", 1},
		{"v2.0.0", "v10.0.0", -1},
		{"1.0.0-rc.1", "1.0.0", -1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta", -1},
		{"1.0.0-alpha.2", "1.0.0-alpha.10", -1},
		{"1.0.0-beta", "1.0.0-alpha", 1},
		{"1.0.0+build.1", "1.0.0+build.2", 0},
		{"dev", "v0.0.1", -1},
		{"", "v0.0.1", -1},
		{"v0.0.1", "dev", 1},
		{"dev", "dev", 0},
		{"v1.x", "v1.0", -1},
		{"1.0.0-", "1.0.0", -1},
		// What "git describe" prints for a commit after a tag is newer than
		// the tag, and older than the next version.
		{"v1.2.3-5-gabc1234", "v1.2.3", 1},
		{"v1.2.3-5-gabc1234", "v1.2.4", -1},
		{"v1.2.3-5-gabc1234", "v1.2.4-rc.1", -1},
		{"v1.2.3-5-gabc1234", "v1.2.3-12-g0123abc", -1},
		{"v1.2.3-5-gabc1234-dirty", "v1.2.3-5-gabc1234", 1},
		{"v1.2.3-dirty", "v1.2.3", 1},
		{"v1.2.3-dirty", "v1.2.3-1-gabc1234", -1},
		{"v1.2.3-0-gabc1234", "v1.2.3", 0},
		{"v1.2.3-rc.1-5-gabc1234", "v1.2.3-rc.1", 1},
		{"v1.2.3-rc.1-5-gabc1234", "v1.2.3-rc.2", -1},
		{"v1.2.3-rc.1-5-gabc1234", "v1.2.3", -1},
		// A pre-release that only looks similar stays one.
		{"1.2.3-5-gamma", "1.2.3", -1},
		{"1.2.3-x-gabc1234", "1.2.3", -1},
		{"1.2.3-5-gabc", "1.2.3", -1},
		{"1.2.4-SNAPSHOT-abc1234", "1.2.3", 1},
		{"1.2.4-SNAPSHOT-abc1234", "1.2.4", -1},
		// A part too large for an int makes the string no version.
		{"v99999999999999999999.0.0", "v1.0.0", -1},
	}
	for _, tt := range tests {
		if got := Compare(tt.a, tt.b); got != tt.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
		if got := Compare(tt.b, tt.a); got != -tt.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tt.b, tt.a, got, -tt.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	if !IsNewer("v1.0.1", "v1.0.0") || IsNewer("v1.0.0", "v1.0.0") || IsNewer("v0.9.0", "v1.0.0") {
		t.Fatal("IsNewer orders releases wrongly")
	}
	if !IsNewer("v0.0.1", "dev") {
		t.Fatal("a local dev build should update to any release")
	}
	// A build of a commit after the release is not updated to that release.
	if IsNewer("v1.2.3", "v1.2.3-5-gabc1234") || IsNewer("v1.2.3", "v1.2.3-dirty") || !IsNewer("v1.2.4", "v1.2.3-5-gabc1234") {
		t.Fatal("IsNewer ranks a build after a tag below that tag, or above the next")
	}
}

func TestStableVersion(t *testing.T) {
	for tag, want := range map[string]bool{
		"v1.2.3":            true,
		"1.2":               true,
		"v1.2.3+build.5":    true,
		"v2.0.0-rc.1":       false,
		"v2.0.0-beta":       false,
		"v1.2.3-5-gabc1234": false,
		"v1.2.3-dirty":      false,
		"nightly":           false,
		"release-2":         false,
		"":                  false,
	} {
		if got := stableVersion(tag); got != want {
			t.Errorf("stableVersion(%q) = %v, want %v", tag, got, want)
		}
	}
}
