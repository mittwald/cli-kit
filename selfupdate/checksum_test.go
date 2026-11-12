package selfupdate

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func checksumsFor(files map[string][]byte) []byte {
	var out []byte
	for name, data := range files {
		out = fmt.Appendf(out, "%x  %s\n", sha256.Sum256(data), name)
	}
	return out
}

func TestVerifyChecksum(t *testing.T) {
	archive := []byte("archive bytes")
	sums := checksumsFor(map[string][]byte{"tool_Linux_x86_64.tar.gz": archive, "other.zip": []byte("x")})
	if err := VerifyChecksum(sums, "tool_Linux_x86_64.tar.gz", archive); err != nil {
		t.Fatalf("VerifyChecksum = %v", err)
	}
	if err := VerifyChecksum(sums, "tool_Linux_x86_64.tar.gz", []byte("tampered")); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("tampered = %v, want ErrChecksumMismatch", err)
	}
	if err := VerifyChecksum(sums, "missing.tar.gz", archive); !errors.Is(err, ErrChecksumMissing) {
		t.Fatalf("missing = %v, want ErrChecksumMissing", err)
	}
	binaryMode := fmt.Appendf(nil, "%x *tool.zip\n", sha256.Sum256(archive))
	if err := VerifyChecksum(binaryMode, "tool.zip", archive); err != nil {
		t.Fatalf("binary-mode line = %v", err)
	}
	if err := VerifyChecksum([]byte("nothex  tool.zip\n"), "tool.zip", archive); err == nil {
		t.Fatal("malformed digest accepted")
	}
	// The name carries the release's tag, which the server chose.
	hostile := "tool-1.2.3\x1b[31m.zip"
	for _, sums := range [][]byte{sums, fmt.Appendf(nil, "%x  %s\n", sha256.Sum256([]byte("other")), hostile)} {
		if err := VerifyChecksum(sums, hostile, archive); err == nil || strings.Contains(err.Error(), "\x1b") {
			t.Fatalf("VerifyChecksum = %q, want an error without control characters", err)
		}
	}
}
