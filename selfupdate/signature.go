package selfupdate

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/crypto/blake2b"
)

// ErrSignature reports that the checksums file of a release carries no
// valid signature by a trusted key: the signature is malformed, was made
// with another key, or does not match the file.
var ErrSignature = errors.New("selfupdate: bad signature")

// maxSignatureSize bounds the signature file download.
const maxSignatureSize = 64 << 10

// Verifier checks the signature of a release's checksums file. Set one as
// Updater.Verifier to install only what a trusted key signed; see
// [Minisign].
type Verifier interface {
	// SignatureName names the signature file of the checksums file called
	// checksums, as it is published beside it in the release.
	SignatureName(checksums string) string
	// Verify returns nil when signature is a valid signature of message
	// by a trusted key, and an error wrapping [ErrSignature] otherwise.
	Verify(message, signature []byte) error
}

// Minisign verifies signatures made with minisign
// (https://jedisct1.github.io/minisign/) or a tool compatible with it,
// such as rsign2 or the minisign packages for Go. It accepts the
// pre-hashed signatures minisign makes by default (algorithm "ED") and the
// legacy ones (algorithm "Ed"), and requires a valid trusted comment
// signature, as minisign does.
//
// The zero value trusts no key. Use [NewMinisign].
type Minisign struct {
	keys []minisignKey
}

type minisignKey struct {
	id  [8]byte
	key ed25519.PublicKey
}

// NewMinisign returns a Minisign that trusts each of publicKeys. A key is
// the Base64 line minisign prints ("RWQ…") or the whole public key file,
// its untrusted comment line included. More than one key lets a release
// signed with a new key verify while the old one is being retired.
func NewMinisign(publicKeys ...string) (*Minisign, error) {
	if len(publicKeys) == 0 {
		return nil, errors.New("selfupdate: no minisign public key given")
	}
	m := &Minisign{}
	for _, text := range publicKeys {
		key, err := parseMinisignKey(text)
		if err != nil {
			return nil, err
		}
		m.keys = append(m.keys, key)
	}
	return m, nil
}

// parseMinisignKey reads a public key: an algorithm ("Ed"), an 8-byte key
// ID and a 32-byte Ed25519 key, Base64-encoded, alone or below the
// untrusted comment of a minisign public key file.
func parseMinisignKey(text string) (minisignKey, error) {
	line := ""
	for l := range strings.Lines(text) {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "untrusted comment:") {
			continue
		}
		if line != "" {
			return minisignKey{}, errors.New("selfupdate: minisign public key has more than one key line")
		}
		line = l
	}
	raw, err := base64.StdEncoding.DecodeString(line)
	if err != nil || len(raw) != 2+8+ed25519.PublicKeySize || string(raw[:2]) != "Ed" {
		return minisignKey{}, errors.New("selfupdate: malformed minisign public key")
	}
	var key minisignKey
	copy(key.id[:], raw[2:10])
	key.key = ed25519.PublicKey(slices.Clone(raw[10:]))
	return key, nil
}

// SignatureName returns checksums with ".minisig" appended, the name
// minisign gives a signature by default.
func (m *Minisign) SignatureName(checksums string) string {
	return checksums + ".minisig"
}

// Verify checks signature, the contents of a minisign signature file,
// against message. The signature has to be made by one of the trusted
// keys, and its trusted comment has to be signed by the same key.
func (m *Minisign) Verify(message, signature []byte) error {
	sig, err := parseMinisignSignature(signature)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(m.keys, func(k minisignKey) bool { return k.id == sig.keyID })
	if i < 0 {
		return fmt.Errorf("%w: made with key %s, which is not trusted", ErrSignature, keyIDString(sig.keyID))
	}
	key := m.keys[i].key
	signed := message
	if sig.prehashed {
		sum := blake2b.Sum512(message)
		signed = sum[:]
	}
	if !ed25519.Verify(key, signed, sig.signature) {
		return fmt.Errorf("%w: the signature does not match the file", ErrSignature)
	}
	global := append(slices.Clone(sig.signature), sig.trustedComment...)
	if !ed25519.Verify(key, global, sig.globalSignature) {
		return fmt.Errorf("%w: the trusted comment is not signed", ErrSignature)
	}
	return nil
}

type minisignSignature struct {
	prehashed       bool
	keyID           [8]byte
	signature       []byte
	trustedComment  []byte
	globalSignature []byte
}

// parseMinisignSignature reads the four lines of a signature file: an
// untrusted comment, the signature (algorithm, key ID, Ed25519
// signature), the trusted comment, and the signature of the signature and
// the trusted comment together.
func parseMinisignSignature(data []byte) (minisignSignature, error) {
	malformed := func(what string) (minisignSignature, error) {
		return minisignSignature{}, fmt.Errorf("%w: malformed minisign signature: %s", ErrSignature, what)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	if len(lines) != 4 {
		return malformed("want 4 lines")
	}
	if !strings.HasPrefix(lines[0], "untrusted comment:") {
		return malformed("no untrusted comment")
	}
	raw, err := base64.StdEncoding.DecodeString(lines[1])
	if err != nil || len(raw) != 2+8+ed25519.SignatureSize {
		return malformed("bad signature line")
	}
	var sig minisignSignature
	switch string(raw[:2]) {
	case "ED":
		sig.prehashed = true
	case "Ed":
	default:
		return malformed("unknown algorithm")
	}
	copy(sig.keyID[:], raw[2:10])
	sig.signature = raw[10:]
	comment, ok := strings.CutPrefix(lines[2], "trusted comment: ")
	if !ok {
		return malformed("no trusted comment")
	}
	sig.trustedComment = []byte(comment)
	sig.globalSignature, err = base64.StdEncoding.DecodeString(lines[3])
	if err != nil || len(sig.globalSignature) != ed25519.SignatureSize {
		return malformed("bad trusted comment signature")
	}
	return sig, nil
}

// keyIDString formats a key ID the way minisign prints it: as the
// hexadecimal number its little-endian bytes make.
func keyIDString(id [8]byte) string {
	reversed := id
	slices.Reverse(reversed[:])
	return fmt.Sprintf("%X", reversed[:])
}
