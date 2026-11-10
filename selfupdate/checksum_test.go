package selfupdate

import (
	"crypto/sha256"
	"errors"
	"fmt"
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
}
