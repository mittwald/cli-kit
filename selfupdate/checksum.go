package selfupdate

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrChecksumMissing reports that the checksums file has no entry for
	// the downloaded archive.
	ErrChecksumMissing = errors.New("selfupdate: no checksum for archive")
	// ErrChecksumMismatch reports that the downloaded archive does not match
	// its checksum: it was corrupted or tampered with.
	ErrChecksumMismatch = errors.New("selfupdate: checksum mismatch")
)

// VerifyChecksum checks data, the downloaded file called name, against
// checksums, a file in the format sha256sum and GoReleaser write: one
// "<hex digest>  <file name>" line per file, where the name may carry a
// leading "*" (binary mode).
func VerifyChecksum(checksums []byte, name string, data []byte) error {
	want, err := findChecksum(checksums, name)
	if err != nil {
		return err
	}
	got := sha256.Sum256(data)
	if subtle.ConstantTimeCompare(got[:], want) != 1 {
		return fmt.Errorf("%w: %s has SHA-256 %x, checksums file says %x", ErrChecksumMismatch, name, got, want)
	}
	return nil
}

func findChecksum(checksums []byte, name string) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(checksums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != sha256.Size {
			return nil, fmt.Errorf("selfupdate: malformed SHA-256 checksum for %s", name)
		}
		return digest, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("selfupdate: read checksums: %w", err)
	}
	return nil, fmt.Errorf("%w %s", ErrChecksumMissing, name)
}
