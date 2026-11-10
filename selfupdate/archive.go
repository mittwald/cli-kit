package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// ErrNoBinary reports that an archive holds no file that can be the
// binary.
var ErrNoBinary = errors.New("selfupdate: no binary in archive")

// MinBinarySize is the smallest file [Extract] accepts as the binary when
// it has to guess which file that is. Every Go binary is larger; README,
// LICENSE and completion files bundled beside it are far smaller.
const MinBinarySize = 1 << 20

// maxEntrySize bounds a single archive entry, so a crafted archive cannot
// exhaust memory.
const maxEntrySize = 1 << 30

type archiveEntry struct {
	name string
	data []byte
}

// Extract returns the binary from archive, a .tar.gz, .tgz or .zip file
// called name. It takes the regular file whose base name is binary (binary
// or binary.exe) when there is one, and otherwise the largest file, as long
// as that is at least [MinBinarySize]. Entries are only read into memory,
// never written to disk, so paths inside the archive cannot escape
// anywhere.
func Extract(name string, archive []byte, binary string) ([]byte, error) {
	var (
		entries []archiveEntry
		err     error
	)
	switch lower := strings.ToLower(name); {
	case strings.HasSuffix(lower, ".zip"):
		entries, err = zipEntries(archive)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		entries, err = tarGzEntries(archive)
	default:
		return nil, fmt.Errorf("selfupdate: unsupported archive type: %s", name)
	}
	if err != nil {
		return nil, err
	}
	return pickBinary(entries, binary)
}

func tarGzEntries(data []byte) ([]archiveEntry, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("selfupdate: ungzip archive: %w", err)
	}
	defer gz.Close()
	var entries []archiveEntry
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return entries, nil
		}
		if err != nil {
			return nil, fmt.Errorf("selfupdate: read tar entry: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		content, err := readEntry(tr, hdr.Name)
		if err != nil {
			return nil, err
		}
		entries = append(entries, archiveEntry{name: hdr.Name, data: content})
	}
}

func zipEntries(data []byte) ([]archiveEntry, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("selfupdate: read zip archive: %w", err)
	}
	var entries []archiveEntry
	for _, f := range zr.File {
		if !f.Mode().IsRegular() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("selfupdate: read zip entry %s: %w", f.Name, err)
		}
		content, err := readEntry(rc, f.Name)
		// A read-only zip entry has nothing to flush; a failed close after a
		// complete read changes nothing about the content.
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		entries = append(entries, archiveEntry{name: f.Name, data: content})
	}
	return entries, nil
}

func readEntry(r io.Reader, name string) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(r, maxEntrySize+1))
	if err != nil {
		return nil, fmt.Errorf("selfupdate: read archive entry %s: %w", name, err)
	}
	if len(content) > maxEntrySize {
		return nil, fmt.Errorf("selfupdate: archive entry %s is larger than %d bytes", name, maxEntrySize)
	}
	return content, nil
}

func pickBinary(entries []archiveEntry, binary string) ([]byte, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: archive is empty", ErrNoBinary)
	}
	if binary != "" {
		for _, e := range entries {
			base := path.Base(strings.ReplaceAll(e.name, `\`, "/"))
			if base == binary || base == binary+".exe" {
				return e.data, nil
			}
		}
	}
	best := entries[0]
	for _, e := range entries[1:] {
		if len(e.data) > len(best.data) {
			best = e
		}
	}
	if len(best.data) < MinBinarySize {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = fmt.Sprintf("%s (%d bytes)", e.name, len(e.data))
		}
		return nil, fmt.Errorf("%w: no file named %q, and the largest file is too small to be one; archive holds %s",
			ErrNoBinary, binary, strings.Join(names, ", "))
	}
	return best.data, nil
}
