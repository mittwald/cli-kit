package theme

import (
	"slices"
	"testing"
)

// The same name keeps one style; different names spread over the palette.
func TestHashed(t *testing.T) {
	alice := Hashed("Alice", nil)
	if alice == "" {
		t.Fatal("a named author must get a style")
	}
	if !slices.Contains(NamePalette, alice) {
		t.Fatalf("Hashed = %q, want one of NamePalette", alice)
	}
	tests := []struct {
		name string
		want Style
	}{
		{" alice ", alice},
		{"ALICE", alice},
		{"", ""},
		{" \t", ""},
	}
	for _, tt := range tests {
		if got := Hashed(tt.name, nil); got != tt.want {
			t.Errorf("Hashed(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
	distinct := map[Style]bool{}
	for _, name := range []string{"alice", "bob", "carol", "dave", "erin", "frank", "grace"} {
		distinct[Hashed(name, nil)] = true
	}
	if len(distinct) < 4 {
		t.Fatalf("seven names got %d styles, want the palette spread out", len(distinct))
	}
}

func TestHashedOwnPalette(t *testing.T) {
	palette := []Style{"31", "32"}
	for _, name := range []string{"alice", "bob", "carol"} {
		if got := Hashed(name, palette); !slices.Contains(palette, got) {
			t.Errorf("Hashed(%q) = %q, want one of %q", name, got, palette)
		}
	}
	if got := Hashed("alice", []Style{"35"}); got != "35" {
		t.Errorf("Hashed with a palette of one = %q, want it", got)
	}
}
