package secret

import (
	"errors"
	"maps"
	"sync"
)

var (
	// ErrNotFound reports that a store holds no secret for an account.
	ErrNotFound = errors.New("secret: not found")
	// ErrEmpty reports an attempt to store or use an empty secret.
	ErrEmpty = errors.New("secret: empty value")
)

// Store keeps one secret per account name.
type Store interface {
	// Get returns the secret stored for account, or an error wrapping
	// ErrNotFound when there is none.
	Get(account string) (string, error)
	// Set stores value for account, replacing any earlier value. It refuses
	// an empty value with ErrEmpty.
	Set(account, value string) error
	// Delete removes the secret stored for account, or returns an error
	// wrapping ErrNotFound when there is none.
	Delete(account string) error
}

// Memory is a Store that keeps secrets in memory, for tests. The zero value
// is an empty store ready to use; it is safe for concurrent use.
type Memory struct {
	mu      sync.Mutex
	secrets map[string]string
}

// NewMemory returns a Memory store holding the given account/secret pairs.
func NewMemory(secrets map[string]string) *Memory {
	m := &Memory{secrets: make(map[string]string, len(secrets))}
	maps.Copy(m.secrets, secrets)
	return m
}

// Get implements Store.
func (m *Memory) Get(account string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.secrets[account]
	if !ok || value == "" {
		return "", ErrNotFound
	}
	return value, nil
}

// Set implements Store.
func (m *Memory) Set(account, value string) error {
	if value == "" {
		return ErrEmpty
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.secrets == nil {
		m.secrets = make(map[string]string)
	}
	m.secrets[account] = value
	return nil
}

// Delete implements Store.
func (m *Memory) Delete(account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.secrets[account]; !ok {
		return ErrNotFound
	}
	delete(m.secrets, account)
	return nil
}

// Fallback returns a Store that uses primary and falls back to secondary
// when primary fails. It suits a keyring that may be missing (a server
// without a Secret Service, a container), with a [FileStore] behind it:
//
//   - Get reads primary, and secondary when primary has no secret or fails.
//   - Set writes primary, and secondary only when primary fails.
//   - Delete removes the account from both, so a secret written to the
//     secondary while the primary was unavailable does not come back. It
//     succeeds when either store removed the secret.
func Fallback(primary, secondary Store) Store {
	return fallback{primary: primary, secondary: secondary}
}

type fallback struct{ primary, secondary Store }

func (f fallback) Get(account string) (string, error) {
	value, err := f.primary.Get(account)
	if err == nil {
		return value, nil
	}
	value, secondaryErr := f.secondary.Get(account)
	if secondaryErr == nil {
		return value, nil
	}
	if errors.Is(err, ErrNotFound) {
		return "", secondaryErr
	}
	if errors.Is(secondaryErr, ErrNotFound) {
		return "", err
	}
	return "", errors.Join(err, secondaryErr)
}

func (f fallback) Set(account, value string) error {
	if value == "" {
		return ErrEmpty
	}
	err := f.primary.Set(account, value)
	if err == nil {
		return nil
	}
	if secondaryErr := f.secondary.Set(account, value); secondaryErr != nil {
		return errors.Join(err, secondaryErr)
	}
	return nil
}

func (f fallback) Delete(account string) error {
	err := f.primary.Delete(account)
	secondaryErr := f.secondary.Delete(account)
	switch {
	case err == nil || secondaryErr == nil:
		// A primary that fails is taken to be unavailable, as in Get and Set.
		return nil
	case errors.Is(err, ErrNotFound) && errors.Is(secondaryErr, ErrNotFound):
		return ErrNotFound
	case errors.Is(err, ErrNotFound):
		return secondaryErr
	case errors.Is(secondaryErr, ErrNotFound):
		return err
	default:
		return errors.Join(err, secondaryErr)
	}
}
