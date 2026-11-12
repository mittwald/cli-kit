package secret

import (
	"errors"
	"fmt"
	"os"
	"testing"

	gokeyring "github.com/zalando/go-keyring"
)

// ErrNoService reports a [Keyring] used without a service name.
var ErrNoService = errors.New("secret: keyring service name is required")

// ErrTestKeyring reports a [Keyring] used in a test binary, which leaves the
// operating system keyring alone; see [Keyring].
var ErrTestKeyring = errors.New("secret: the operating system keyring is not used in tests")

// TestKeyringEnv names the environment variable that lets a test binary use
// the operating system keyring: tests run with CLI_KIT_TEST_KEYRING=system
// reach it.
const TestKeyringEnv = "CLI_KIT_TEST_KEYRING"

func init() {
	guardTestKeyring(testing.Testing(), os.Getenv(TestKeyringEnv))
}

// guardTestKeyring makes every keyring call of a test binary fail with
// ErrTestKeyring, unless the test run asked for the operating system
// keyring. go-keyring is switched to its mock before any test runs, so a
// test that installs a mock of its own (gokeyring.MockInit) still gets that
// one. It reports whether it switched.
func guardTestKeyring(inTest bool, env string) bool {
	if !inTest || env == "system" {
		return false
	}
	gokeyring.MockInitWithError(ErrTestKeyring)
	return true
}

// Keyring is a Store backed by the operating system keyring: the macOS
// Keychain, the Windows Credential Manager, or a Secret Service provider
// (GNOME Keyring, KWallet) over D-Bus on Linux and BSD.
//
// Service names the application, such as "example-tool"; every account is
// stored under it. Use a second service name, such as "example-tool-user",
// to keep unrelated values, such as a user name, apart from passwords.
//
// Where no keyring is available, every method fails with an error that does
// not wrap ErrNotFound; wrap the Keyring in [Fallback] to keep working.
//
// A test binary (see [testing.Testing]) never reaches the operating system
// keyring, so no test can read, overwrite or delete the secrets of whoever
// runs it: every method fails with an error wrapping [ErrTestKeyring], as if
// there were no keyring, and a [Fallback] moves on to its secondary store.
// This is done by switching the go-keyring package, which Keyring calls, to
// its mock when the test binary starts. A test that needs a working keyring
// uses a [Memory] store in its place, or installs go-keyring's in-memory mock
// with gokeyring.MockInit. Tests that are meant to reach the operating
// system keyring run with the environment variable [TestKeyringEnv] set to
// "system".
type Keyring struct {
	// Service is the keyring service name. It is required.
	Service string
}

// Get implements Store. A stored empty value, which only another tool can
// write, counts as no secret.
func (k Keyring) Get(account string) (string, error) {
	if k.Service == "" {
		return "", ErrNoService
	}
	value, err := gokeyring.Get(k.Service, account)
	if errors.Is(err, gokeyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("secret: read keyring service %q: %w", k.Service, err)
	}
	if value == "" {
		return "", ErrNotFound
	}
	return value, nil
}

// Set implements Store.
func (k Keyring) Set(account, value string) error {
	if k.Service == "" {
		return ErrNoService
	}
	if value == "" {
		return ErrEmpty
	}
	if err := gokeyring.Set(k.Service, account, value); err != nil {
		return fmt.Errorf("secret: write keyring service %q: %w", k.Service, err)
	}
	return nil
}

// Delete implements Store.
func (k Keyring) Delete(account string) error {
	if k.Service == "" {
		return ErrNoService
	}
	err := gokeyring.Delete(k.Service, account)
	if errors.Is(err, gokeyring.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("secret: delete from keyring service %q: %w", k.Service, err)
	}
	return nil
}
