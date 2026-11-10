package secret

import (
	"errors"
	"fmt"

	gokeyring "github.com/zalando/go-keyring"
)

// ErrNoService reports a [Keyring] used without a service name.
var ErrNoService = errors.New("secret: keyring service name is required")

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
