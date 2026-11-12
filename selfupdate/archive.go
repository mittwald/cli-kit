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
// binary: none of the right name or size, or one that is no executable for
// the platform.
var ErrNoBinary = errors.New("selfupdate: no binary in archive")

// MinBinarySize is the smallest file [Extract] accepts as the binary when
// it has to guess which file that is. Every Go binary is larger; README,
// LICENSE and completion files bundled beside it are far smaller.
const MinBinarySize = 1 << 20

// archiveLimits bound what an archive may expand to, so a crafted one can
// exhaust neither memory nor time. One entry is held in memory at a time:
// the best candidate for the binary so far, which is at most entry bytes
// large.
type archiveLimits struct {
	// entry bounds the size of an entry taken as a candidate.
	entry int64
	// total bounds what is decompressed in all: every entry of a .tar.gz,
	// which has to be decompressed to be skipped, and every entry of a .zip
	// that is read.
	total int64
	// entries bounds the number of entries.
	entries int
}

// defaultLimits are the limits [ExtractFor] applies.
var defaultLimits = archiveLimits{entry: 1 << 30, total: 4 << 30, entries: 100_000}

// maxListedEntries bounds the entries an error message names.
const maxListedEntries = 10

// Extract returns the binary for the running platform from archive; see
// [ExtractFor].
func Extract(name string, archive []byte, binary string) ([]byte, error) {
	return ExtractFor(CurrentPlatform(), name, archive, binary)
}

// ExtractFor returns the binary for platform p from archive, a .tar.gz,
// .tgz or .zip file called name; the zero Platform means
// [CurrentPlatform]. It takes the regular file whose base name is binary,
// or binary.exe when p is Windows: the one nearest the archive's root when
// there are several, as a completion script in a subdirectory may carry
// the binary's name, and the largest of those equally near. When no file
// has that name it takes the largest file, as long as that is at least
// [MinBinarySize].
//
// The file taken has to be an executable for p: an ELF file, a Mach-O file
// (universal ones included) for macOS and iOS, a PE file for Windows.
// Anything else is refused with an error wrapping [ErrNoBinary]. A
// platform with another format (AIX, Plan 9, WebAssembly) is not checked.
//
// Entries are only read into memory, never written to disk, so paths
// inside the archive cannot escape anywhere, and only the best candidate
// so far is kept. An archive with more than 100,000 entries, with a binary
// larger than 1 GiB, or one that expands to more than 4 GiB is refused.
func ExtractFor(p Platform, name string, archive []byte, binary string) ([]byte, error) {
	return extract(p, name, archive, binary, defaultLimits)
}

func extract(p Platform, name string, archive []byte, binary string, limits archiveLimits) ([]byte, error) {
	if p == (Platform{}) {
		p = CurrentPlatform()
	}
	pick := &picker{platform: p, binary: binary, limits: limits}
	var err error
	switch lower := strings.ToLower(name); {
	case strings.HasSuffix(lower, ".zip"):
		err = scanZip(archive, pick)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		err = scanTarGz(archive, pick)
	default:
		return nil, fmt.Errorf("selfupdate: unsupported archive type: %s", plain(name))
	}
	if err != nil {
		return nil, err
	}
	return pick.result()
}

// picker chooses the binary among the entries of an archive as they pass
// by, keeping the content of the best candidate so far only.
type picker struct {
	platform Platform
	// binary is the wanted base name; empty when only size can tell.
	binary string
	limits archiveLimits

	// found reports that a candidate is held. named tells whether it has
	// the binary's name, in which case no entry of another name replaces
	// it; depth is the number of directories it lies in.
	found bool
	named bool
	depth int
	name  string
	data  []byte

	// files counts the regular files seen; listed names the first of them
	// for an error message.
	files  int
	listed []string
	// tooLarge names the first file above the entry limit that was passed
	// over, as it did not have the binary's name.
	tooLarge string
}

// wants reports whether the regular file called name, size bytes large,
// is a better candidate than the one held, going by name and size alone,
// so that an entry that is not is never read.
func (p *picker) wants(name string, size int64) bool {
	p.files++
	if len(p.listed) < maxListedEntries {
		p.listed = append(p.listed, fmt.Sprintf("%s (%d bytes)", plain(name), size))
	}
	switch named := p.hasBinaryName(name); {
	case named && !p.named:
		return true
	case named:
		depth := entryDepth(name)
		return depth < p.depth || depth == p.depth && size > int64(len(p.data))
	case p.named:
		return false
	case size > p.limits.entry:
		// Not worth failing for while a file of the binary's name may still
		// come; result reports it when none did.
		if p.tooLarge == "" {
			p.tooLarge = name
		}
		return false
	default:
		return size >= MinBinarySize && size > int64(len(p.data))
	}
}

// take reads the entry wants accepted and holds it as the candidate. The
// previous candidate is let go first, so two are never held at once.
func (p *picker) take(name string, size int64, r io.Reader) error {
	if size > p.limits.entry {
		return fmt.Errorf("selfupdate: archive entry %s is larger than %d bytes", plain(name), p.limits.entry)
	}
	p.found, p.data = false, nil
	content, err := io.ReadAll(io.LimitReader(r, p.limits.entry+1))
	if err != nil {
		return fmt.Errorf("selfupdate: read archive entry %s: %w", plain(name), err)
	}
	if int64(len(content)) > p.limits.entry {
		return fmt.Errorf("selfupdate: archive entry %s is larger than %d bytes", plain(name), p.limits.entry)
	}
	p.found, p.named, p.depth, p.name, p.data = true, p.hasBinaryName(name), entryDepth(name), name, content
	return nil
}

// hasBinaryName reports whether the entry's base name is the binary's.
func (p *picker) hasBinaryName(name string) bool {
	if p.binary == "" {
		return false
	}
	base := path.Base(slashed(name))
	return base == p.binary || p.platform.OS == "windows" && base == p.binary+".exe"
}

// result returns the candidate held, when it is an executable for the
// platform.
func (p *picker) result() ([]byte, error) {
	switch {
	case p.files == 0:
		return nil, fmt.Errorf("%w: archive is empty", ErrNoBinary)
	case !p.named && p.tooLarge != "":
		// The largest file would be the binary, and it is too large.
		return nil, fmt.Errorf("selfupdate: archive entry %s is larger than %d bytes", plain(p.tooLarge), p.limits.entry)
	case !p.found:
		holds := strings.Join(p.listed, ", ")
		if more := p.files - len(p.listed); more > 0 {
			holds += fmt.Sprintf(" and %d more", more)
		}
		return nil, fmt.Errorf("%w: no file named %q, and the largest file is too small to be one; archive holds %s",
			ErrNoBinary, p.binary, holds)
	}
	if format := executableFormat(p.platform.OS); format != "" && !hasMagic(format, p.data) {
		return nil, fmt.Errorf("%w: %s is not an executable for %s (no %s file)",
			ErrNoBinary, plain(p.name), plain(p.platform.OS), format)
	}
	return p.data, nil
}

// slashed returns an entry name with forward slashes, as some zip tools
// write backslashes.
func slashed(name string) string { return strings.ReplaceAll(name, `\`, "/") }

// entryDepth returns the number of directories an entry lies in.
func entryDepth(name string) int {
	return strings.Count(strings.TrimPrefix(path.Clean(slashed(name)), "/"), "/")
}

// Executable file formats.
const (
	formatELF   = "ELF"
	formatMachO = "Mach-O"
	formatPE    = "PE"
)

// executableFormat returns the executable file format of an operating
// system, by its GOOS name, or "" for one whose format is not checked.
func executableFormat(goos string) string {
	switch goos {
	case "windows":
		return formatPE
	case "darwin", "ios":
		return formatMachO
	case "aix", "plan9", "js", "wasip1":
		return ""
	}
	return formatELF
}

// machOMagics are the first four bytes of a Mach-O file: 32 and 64 bit in
// either byte order, and the universal ("fat") file that holds several.
var machOMagics = [][]byte{
	{0xfe, 0xed, 0xfa, 0xce}, {0xce, 0xfa, 0xed, 0xfe},
	{0xfe, 0xed, 0xfa, 0xcf}, {0xcf, 0xfa, 0xed, 0xfe},
	{0xca, 0xfe, 0xba, 0xbe}, {0xca, 0xfe, 0xba, 0xbf},
}

// hasMagic reports whether data starts the way a file of format does.
func hasMagic(format string, data []byte) bool {
	switch format {
	case formatELF:
		return bytes.HasPrefix(data, []byte("\x7fELF"))
	case formatPE:
		return bytes.HasPrefix(data, []byte("MZ"))
	case formatMachO:
		for _, magic := range machOMagics {
			if bytes.HasPrefix(data, magic) {
				return true
			}
		}
	}
	return false
}

// errTooLarge reports an archive that expands beyond the limit.
func errTooLarge(limit int64) error {
	return fmt.Errorf("selfupdate: archive expands to more than %d bytes", limit)
}

// cappedReader fails once more than limit bytes were read from r; left is
// what remains of the limit.
type cappedReader struct {
	r           io.Reader
	limit, left int64
}

func (c *cappedReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		// Reading on tells an archive that ends here from one that goes on.
		var one [1]byte
		if n, err := c.r.Read(one[:]); n == 0 {
			return 0, err
		}
		return 0, errTooLarge(c.limit)
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}

func scanTarGz(data []byte, pick *picker) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("selfupdate: ungzip archive: %w", err)
	}
	defer gz.Close()
	// Skipping an entry decompresses it too, so the cap is on the stream.
	tr := tar.NewReader(&cappedReader{r: gz, limit: pick.limits.total, left: pick.limits.total})
	for entries := 0; ; entries++ {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("selfupdate: read tar entry: %w", err)
		}
		if entries >= pick.limits.entries {
			return fmt.Errorf("selfupdate: archive has more than %d entries", pick.limits.entries)
		}
		if hdr.Typeflag != tar.TypeReg || !pick.wants(hdr.Name, hdr.Size) {
			continue
		}
		if err := pick.take(hdr.Name, hdr.Size, tr); err != nil {
			return err
		}
	}
}

func scanZip(data []byte, pick *picker) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("selfupdate: read zip archive: %w", err)
	}
	if len(zr.File) > pick.limits.entries {
		return fmt.Errorf("selfupdate: archive has more than %d entries", pick.limits.entries)
	}
	var total int64
	for _, f := range zr.File {
		// The size the entry declares is the size it has: reading fails
		// when the content is longer or shorter.
		// An entry above the limit is refused by its declared size alone,
		// so what the size is beyond that does not matter.
		size := f.FileInfo().Size()
		if size < 0 || size > pick.limits.entry {
			size = pick.limits.entry + 1
		}
		if !f.Mode().IsRegular() || !pick.wants(f.Name, size) {
			continue
		}
		if total += size; total > pick.limits.total {
			return errTooLarge(pick.limits.total)
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("selfupdate: read zip entry %s: %w", plain(f.Name), err)
		}
		err = pick.take(f.Name, size, rc)
		// A read-only zip entry has nothing to flush; a failed close after a
		// complete read changes nothing about the content.
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
