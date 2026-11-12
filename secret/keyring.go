package secret

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	gokeyring "github.com/zalando/go-keyring"
)

// ErrNoService reports a [Keyring] used without a service name.
var ErrNoService = errors.New("secret: keyring service name is required")

// ErrTestKeyring reports a [Keyring] used in a test binary, which leaves the
// operating system keyring alone; see [Keyring].
var ErrTestKeyring = errors.New("secret: the operating system keyring is not used in tests")

// ErrKeyringTimeout reports that the operating system keyring did not
// answer within [Keyring.Timeout].
var ErrKeyringTimeout = errors.New("secret: the keyring did not answer in time")

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
// not wrap ErrNotFound; wrap the Keyring in [Fallback] to keep working. A
// keyring also refuses a secret that is too large for it, a few kilobytes
// on macOS and Windows; behind a Fallback such a secret goes to the
// secondary store.
//
// Without a Timeout a call waits for the keyring as long as it takes. That
// is right on a desktop, where the keyring may first ask the user to unlock
// it, and wrong where nobody can answer: in an SSH session on a machine
// whose keyring is locked, the unlock dialog opens on a screen no one is
// looking at, and the call never returns. A tool that also runs in such
// places sets Timeout, so that the call gives up and a [Fallback] moves on.
// On Linux, a machine without a D-Bus session may first start a session
// bus of its own (dbus-launch, when that program is installed) on every
// call; the Timeout covers that wait as well.
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
	// Timeout is how long Get, Set and Delete each wait for the keyring to
	// answer; zero means as long as it takes. Leave room for a person to
	// type the password of a locked keyring, half a minute or more. When
	// it runs out, the method fails with an error wrapping
	// [ErrKeyringTimeout], not [ErrNotFound], as if there were no keyring.
	// The call to the keyring is abandoned, not undone: a Set or Delete
	// that the keyring carries out later still takes effect.
	Timeout time.Duration
}

// The calls into go-keyring, replaced in tests.
var (
	keyringGet    = gokeyring.Get
	keyringSet    = gokeyring.Set
	keyringDelete = gokeyring.Delete
)

// call runs one call into the keyring, for at most Timeout when that is
// set. A call that does not come back in time keeps waiting on a goroutine
// of its own; there is no way to cancel it.
func (k Keyring) call(do func() (string, error)) (string, error) {
	if k.Timeout <= 0 {
		return do()
	}
	type result struct {
		value string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := do()
		done <- result{value, err}
	}()
	timer := time.NewTimer(k.Timeout)
	defer timer.Stop()
	select {
	case r := <-done:
		return r.value, r.err
	case <-timer.C:
		return "", ErrKeyringTimeout
	}
}

// failed words a failed keyring call; action is what was tried, such as
// "read".
func (k Keyring) failed(action string, err error) error {
	if errors.Is(err, ErrKeyringTimeout) {
		return fmt.Errorf("%w: service %q, waited %s", ErrKeyringTimeout, k.Service, k.Timeout)
	}
	return fmt.Errorf("secret: %s keyring service %q: %w", action, k.Service, err)
}

// Get implements Store. A stored empty value, which only another tool can
// write, counts as no secret.
func (k Keyring) Get(account string) (string, error) {
	if k.Service == "" {
		return "", ErrNoService
	}
	get := keyringGet
	value, err := k.call(func() (string, error) { return get(k.Service, account) })
	if errors.Is(err, gokeyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", k.failed("read", err)
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
	set := keyringSet
	if _, err := k.call(func() (string, error) { return "", set(k.Service, account, value) }); err != nil {
		return k.failed("write", err)
	}
	return nil
}

// Delete implements Store.
func (k Keyring) Delete(account string) error {
	if k.Service == "" {
		return ErrNoService
	}
	remove := keyringDelete
	_, err := k.call(func() (string, error) { return "", remove(k.Service, account) })
	if errors.Is(err, gokeyring.ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return k.failed("delete from", err)
	}
	return nil
}
