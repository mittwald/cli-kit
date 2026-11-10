package selfupdate

import "testing"

func TestAssetNamers(t *testing.T) {
	rel := Release{Tag: "v1.2.3"}
	tests := []struct {
		platform           Platform
		titleCase, version string
	}{
		{Platform{OS: "linux", Arch: "amd64"}, "tool_Linux_x86_64.tar.gz", "tool-1.2.3-linux-amd64.tar.gz"},
		{Platform{OS: "linux", Arch: "arm64"}, "tool_Linux_arm64.tar.gz", "tool-1.2.3-linux-arm64.tar.gz"},
		{Platform{OS: "linux", Arch: "arm", Arm: "7"}, "tool_Linux_armv7.tar.gz", "tool-1.2.3-linux-armv7.tar.gz"},
		{Platform{OS: "darwin", Arch: "arm64"}, "tool_Darwin_arm64.tar.gz", "tool-1.2.3-darwin-arm64.tar.gz"},
		{Platform{OS: "windows", Arch: "386"}, "tool_Windows_i386.zip", "tool-1.2.3-windows-386.zip"},
	}
	for _, tt := range tests {
		if got := TitleCaseAssets("tool")(rel, tt.platform); got != tt.titleCase {
			t.Errorf("TitleCaseAssets(%+v) = %q, want %q", tt.platform, got, tt.titleCase)
		}
		if got := VersionedAssets("tool")(rel, tt.platform); got != tt.version {
			t.Errorf("VersionedAssets(%+v) = %q, want %q", tt.platform, got, tt.version)
		}
	}
}

func TestCurrentPlatform(t *testing.T) {
	p := CurrentPlatform()
	if p.OS == "" || p.Arch == "" {
		t.Fatalf("CurrentPlatform() = %+v", p)
	}
	if (p.Arch == "arm") != (p.Arm != "") {
		t.Fatalf("Arm set for a non-arm platform or missing for arm: %+v", p)
	}
}

func TestReleaseVersion(t *testing.T) {
	for tag, want := range map[string]string{"v1.2.3": "1.2.3", "V1.2.3": "1.2.3", "1.2.3": "1.2.3"} {
		if got := (Release{Tag: tag}).Version(); got != want {
			t.Errorf("Version(%q) = %q, want %q", tag, got, want)
		}
	}
}
