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
}
