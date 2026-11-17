package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// Compare is an order: a version equals itself, and swapping the
// arguments flips the result.
func FuzzCompare(f *testing.F) {
	for _, seed := range [][2]string{
		{"v1.2.3", "1.2.3"}, {"1.0.0-rc.1", "1.0.0"}, {"v1.2.3-5-gabc1234", "v1.2.3"},
		{"dev", "v0.0.1"}, {"1.0.0-alpha.1", "1.0.0-alpha.beta"}, {"v99999999999999999999.0.0", "v1.0.0"},
	} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		if got := Compare(a, a); got != 0 {
			t.Fatalf("Compare(%q, %q) = %d", a, a, got)
		}
		if ab, ba := Compare(a, b), Compare(b, a); ab != -ba {
			t.Fatalf("Compare(%q, %q) = %d but Compare(%q, %q) = %d", a, b, ab, b, a, ba)
		}
	})
}

// fuzzArchives returns a tar.gz and a zip that each hold an ELF binary
// called tool, as seeds.
func fuzzArchives(f *testing.F) (tgz, zipped []byte) {
	f.Helper()
	binary := []byte("\x7fELF\x02\x01\x01 binary")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "tool", Size: int64(len(binary)), Mode: 0o755, Typeflag: tar.TypeReg}); err != nil {
		f.Fatal(err)
	}
	if _, err := tw.Write(binary); err != nil {
		f.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		f.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		f.Fatal(err)
	}
	tgz = bytes.Clone(buf.Bytes())
	buf.Reset()
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("tool.exe")
	if err != nil {
		f.Fatal(err)
	}
	if _, err := w.Write([]byte("MZ binary")); err != nil {
		f.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		f.Fatal(err)
	}
	return tgz, buf.Bytes()
}

// An archive from a release is untrusted until it is verified, and
// ExtractFor reads it anyway when SkipChecksum is set: whatever it holds,
// extracting fails cleanly or returns an executable for the platform.
func FuzzExtract(f *testing.F) {
	tgz, zipped := fuzzArchives(f)
	f.Add(tgz, false)
	f.Add(zipped, true)
	f.Fuzz(func(t *testing.T, archive []byte, windows bool) {
		p, name := Platform{OS: "linux", Arch: "amd64"}, "tool.tar.gz"
		if windows {
			p, name = Platform{OS: "windows", Arch: "amd64"}, "tool.zip"
		}
		binary, err := ExtractFor(p, name, archive, "tool")
		if err != nil {
			return
		}
		magic := "\x7fELF"
		if windows {
			magic = "MZ"
		}
		if !bytes.HasPrefix(binary, []byte(magic)) {
			t.Fatalf("ExtractFor returned %q, which is no %s executable", binary, p.OS)
		}
	})
}

// A signature that was not made with the trusted key never verifies, and
// nothing in a signature file makes Verify panic.
func FuzzMinisignVerify(f *testing.F) {
	seed := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	signer := &minisigner{id: [8]byte{1, 2, 3, 4, 5, 6, 7, 8}, priv: seed, pub: seed.Public().(ed25519.PublicKey)}
	message := []byte("abc  tool.tar.gz\n")
	m, err := NewMinisign(signer.publicKey())
	if err != nil {
		f.Fatal(err)
	}
	good := signer.sign(message, false)
	f.Add(message, good)
	f.Add(message, signer.sign(message, true))
	f.Add([]byte("other"), good)
	f.Fuzz(func(t *testing.T, msg, signature []byte) {
		if m.Verify(msg, signature) != nil {
			return
		}
		// Ed25519 signatures are deterministic, and Verify accepts only
		// the canonical one, so an accepted signature is the one the key
		// makes for msg, with whatever comments around it.
		sig, err := parseMinisignSignature(signature)
		if err != nil {
			t.Fatalf("Verify accepted %q, which does not parse: %v", signature, err)
		}
		want, err := parseMinisignSignature(signer.sign(msg, !sig.prehashed))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(sig.signature, want.signature) {
			t.Fatalf("Verify accepted %q for %q, not the key's signature", signature, msg)
		}
	})
}

// A checksums file from elsewhere never makes VerifyChecksum panic, and it
// accepts only data whose SHA-256 the file names.
func FuzzVerifyChecksum(f *testing.F) {
	data := []byte("archive")
	f.Add([]byte("ee5c1aaa0e0bd2c0f2b6ec6ee5fd4e2d5d0b8e6f4b1a6c8d2e9f0a1b2c3d4e5f  tool.tar.gz\n"), data)
	f.Add(checksumsFor(map[string][]byte{"tool.tar.gz": data}), data)
	f.Add([]byte("zz  tool.tar.gz\n*"), []byte{})
	f.Fuzz(func(t *testing.T, checksums, data []byte) {
		if VerifyChecksum(checksums, "tool.tar.gz", data) != nil {
			return
		}
		if !bytes.Contains(bytes.ToLower(checksums), []byte(sha256Hex(data))) {
			t.Fatalf("VerifyChecksum accepted %q for data whose SHA-256 it does not hold", checksums)
		}
	})
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
