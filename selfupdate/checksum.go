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
	// its entry in the checksums file: it was corrupted, or altered on the
	// way from the release. The opposite does not hold; see
	// [VerifyChecksum] for what a matching checksum proves.
	ErrChecksumMismatch = errors.New("selfupdate: checksum mismatch")
)

// VerifyChecksum checks data, the downloaded file called name, against
// checksums, a file in the format sha256sum and GoReleaser write: one
// "<hex digest>  <file name>" line per file, where the name may carry a
// leading "*" (binary mode). Only SHA-256 digests are understood.
//
// A match proves that data is the file the checksums file describes: it
// was not corrupted, and not altered on a transport or storage hop
// between the release and here. It proves nothing about who made the
// file. An [Updater] fetches the checksums file from the same release as
// the archive, and the file carries no signature, so somebody who can
// write the release's files can replace both, and the pair verifies. The
// update is as trustworthy as the place the release is published in and
// the TLS connection to it, not more.
func VerifyChecksum(checksums []byte, name string, data []byte) error {
	want, err := findChecksum(checksums, name)
	if err != nil {
		return err
	}
	got := sha256.Sum256(data)
	if subtle.ConstantTimeCompare(got[:], want) != 1 {
		return fmt.Errorf("%w: %s has SHA-256 %x, checksums file says %x", ErrChecksumMismatch, plain(name), got, want)
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
			return nil, fmt.Errorf("selfupdate: malformed SHA-256 checksum for %s", plain(name))
		}
		return digest, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("selfupdate: read checksums: %w", err)
	}
	return nil, fmt.Errorf("%w %s", ErrChecksumMissing, plain(name))
}
