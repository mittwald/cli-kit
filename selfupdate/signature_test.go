package selfupdate

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/blake2b"
)

// minisigner signs the way minisign does, for tests.
type minisigner struct {
	id   [8]byte
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
}

func newMinisigner(t *testing.T, id byte) *minisigner {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &minisigner{id: [8]byte{id, 2, 3, 4, 5, 6, 7, 8}, priv: priv, pub: pub}
}

// publicKey returns the public key file minisign writes.
func (s *minisigner) publicKey() string {
	raw := append(append([]byte("Ed"), s.id[:]...), s.pub...)
	return "untrusted comment: minisign public key\n" + base64.StdEncoding.EncodeToString(raw) + "\n"
}

// sign returns the signature file of message, pre-hashed unless legacy.
func (s *minisigner) sign(message []byte, legacy bool) []byte {
	alg, signed := "ED", message
	if legacy {
		alg = "Ed"
	} else {
		sum := blake2b.Sum512(message)
		signed = sum[:]
	}
	sig := ed25519.Sign(s.priv, signed)
	const comment = "timestamp:1700000000\tfile:checksums.txt\thashed"
	global := ed25519.Sign(s.priv, append(append([]byte{}, sig...), comment...))
	line := append(append([]byte(alg), s.id[:]...), sig...)
	return []byte("untrusted comment: signature from minisign secret key\n" +
		base64.StdEncoding.EncodeToString(line) + "\n" +
		"trusted comment: " + comment + "\n" +
		base64.StdEncoding.EncodeToString(global) + "\n")
}

func TestMinisignVerifies(t *testing.T) {
	signer := newMinisigner(t, 1)
	message := []byte("abc  tool.tar.gz\n")
	for _, legacy := range []bool{false, true} {
		m, err := NewMinisign(signer.publicKey())
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Verify(message, signer.sign(message, legacy)); err != nil {
			t.Errorf("legacy=%v: Verify = %v", legacy, err)
		}
	}
	// The Base64 line alone is a key too, and CRLF line ends are read.
	line := strings.Split(signer.publicKey(), "\n")[1]
	m, err := NewMinisign(line)
	if err != nil {
		t.Fatal(err)
	}
	crlf := strings.ReplaceAll(string(signer.sign(message, false)), "\n", "\r\n")
	if err := m.Verify(message, []byte(crlf)); err != nil {
		t.Errorf("Verify with CRLF = %v", err)
	}
}

func TestMinisignRejects(t *testing.T) {
	signer := newMinisigner(t, 1)
	other := newMinisigner(t, 2)
	impostor := newMinisigner(t, 1) // the trusted key's ID, another key
	message := []byte("abc  tool.tar.gz\n")
	m, err := NewMinisign(signer.publicKey())
	if err != nil {
		t.Fatal(err)
	}
	good := string(signer.sign(message, false))
	lines := strings.Split(good, "\n")
	for _, tc := range []struct {
		name      string
		message   string
		signature string
		want      string
	}{
		{"changed file", "abd  tool.tar.gz\n", good, "does not match"},
		{"untrusted key", string(message), string(other.sign(message, false)), "0807060504030202, which is not trusted"},
		{"same key ID, other key", string(message), string(impostor.sign(message, false)), "does not match"},
		{"changed trusted comment", string(message), strings.Replace(good, "hashed", "hushed", 1), "trusted comment is not signed"},
		{"empty", string(message), "", "want 4 lines"},
		{"no untrusted comment", string(message), strings.Replace(good, "untrusted comment:", "x:", 1), "no untrusted comment"},
		{"no trusted comment", string(message), strings.Replace(good, "trusted comment: t", "comment: t", 1), "no trusted comment"},
		{"bad signature line", string(message), strings.Replace(good, lines[1], "!!", 1), "bad signature line"},
		{"bad global signature", string(message), strings.Replace(good, lines[3], "AAAA", 1), "bad trusted comment signature"},
		{"unknown algorithm", string(message), strings.Replace(good, lines[1], base64.StdEncoding.EncodeToString(append([]byte("XX"), make([]byte, 72)...)), 1), "unknown algorithm"},
	} {
		err := m.Verify([]byte(tc.message), []byte(tc.signature))
		if !errors.Is(err, ErrSignature) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Verify = %v, want ErrSignature with %q", tc.name, err, tc.want)
		}
	}
	if err := (&Minisign{}).Verify(message, []byte(good)); !errors.Is(err, ErrSignature) {
		t.Errorf("zero Minisign: Verify = %v, want ErrSignature", err)
	}
}

func TestMinisignTrustsEveryKeyGiven(t *testing.T) {
	old, current := newMinisigner(t, 1), newMinisigner(t, 2)
	m, err := NewMinisign(old.publicKey(), current.publicKey())
	if err != nil {
		t.Fatal(err)
	}
	message := []byte("abc  tool.tar.gz\n")
	for _, s := range []*minisigner{old, current} {
		if err := m.Verify(message, s.sign(message, false)); err != nil {
			t.Errorf("key %x: Verify = %v", s.id, err)
		}
	}
}

func TestNewMinisignRejectsBadKeys(t *testing.T) {
	signer := newMinisigner(t, 1)
	line := strings.Split(signer.publicKey(), "\n")[1]
	for _, key := range []string{
		"",
		"untrusted comment: nothing else\n",
		"not base64!",
		base64.StdEncoding.EncodeToString([]byte("Ed short")),
		strings.Replace(line, line[:2], "RX", 1), // another algorithm
		line + "\n" + line,
	} {
		if _, err := NewMinisign(key); err == nil {
			t.Errorf("NewMinisign(%q) accepted a bad key", key)
		}
	}
	if _, err := NewMinisign(); err == nil {
		t.Error("NewMinisign() accepted no key")
	}
	if got := (&Minisign{}).SignatureName("checksums.txt"); got != "checksums.txt.minisig" {
		t.Errorf("SignatureName = %q", got)
	}
}

func signedFixture(t *testing.T) (*Updater, *memorySource, string, *minisigner) {
	t.Helper()
	u, source, exe := newFixture(t)
	signer := newMinisigner(t, 1)
	source.files["checksums.txt.minisig"] = signer.sign(source.files["checksums.txt"], false)
	verifier, err := NewMinisign(signer.publicKey())
	if err != nil {
		t.Fatal(err)
	}
	u.Verifier = verifier
	return u, source, exe, signer
}

func TestUpdateVerifiesTheSignature(t *testing.T) {
	u, _, exe, _ := signedFixture(t)
	if result, err := u.Update(context.Background(), false); err != nil || !result.Updated {
		t.Fatalf("Update = %+v, %v", result, err)
	}
	if got := contents(t, exe); got != newBinary {
		t.Fatalf("executable holds %q", got)
	}
}

func TestUpdateRefusesAnUnsignedOrForgedRelease(t *testing.T) {
	// Somebody who can write the release replaces archive and checksums
	// file together; the signature they cannot make gives them away.
	u, source, exe, _ := signedFixture(t)
	archive := tarGz(t, map[string][]byte{"tool": []byte("\x7fELF evil")})
	source.files["tool_Linux_x86_64.tar.gz"] = archive
	source.files["checksums.txt"] = checksumsFor(map[string][]byte{"tool_Linux_x86_64.tar.gz": archive})
	if _, err := u.Update(context.Background(), false); !errors.Is(err, ErrSignature) {
		t.Fatalf("Update of a forged release = %v, want ErrSignature", err)
	}
	// Signed with their own key.
	source.files["checksums.txt.minisig"] = newMinisigner(t, 9).sign(source.files["checksums.txt"], false)
	if _, err := u.Update(context.Background(), false); !errors.Is(err, ErrSignature) {
		t.Fatalf("Update signed by another key = %v, want ErrSignature", err)
	}
	// No signature at all.
	delete(source.files, "checksums.txt.minisig")
	if _, err := u.Update(context.Background(), false); !errors.Is(err, ErrAssetNotFound) || !strings.Contains(err.Error(), "fetch signature") {
		t.Fatalf("Update without a signature = %v, want the missing signature reported", err)
	}
	if got := contents(t, exe); got != "old binary" {
		t.Fatalf("executable replaced: %q", got)
	}
}

func TestVerifierExcludesSkipChecksum(t *testing.T) {
	u, source, _, _ := signedFixture(t)
	u.SkipChecksum = true
	if _, err := u.Update(context.Background(), false); err == nil || !strings.Contains(err.Error(), "SkipChecksum") {
		t.Fatalf("Update = %v, want Verifier and SkipChecksum refused", err)
	}
	if source.calls != 0 {
		t.Fatal("asked the source before refusing the configuration")
	}
}
