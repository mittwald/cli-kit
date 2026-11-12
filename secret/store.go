package secret

import (
	"errors"
	"fmt"
	"maps"
	"sync"
)

var (
	// ErrNotFound reports that a store holds no secret for an account.
	ErrNotFound = errors.New("secret: not found")
	// ErrEmpty reports an attempt to store or use an empty secret.
	ErrEmpty = errors.New("secret: empty value")
	// ErrTooLong reports a secret longer than its limit: a line longer than
	// [Resolver.MaxLen] in a file or on stdin, or a value or file larger
	// than a [FileStore] keeps.
	ErrTooLong = errors.New("secret: too long")
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
//     When secondary has no secret either, the error wraps [ErrNotFound],
//     also when primary failed, so a [Resolver] goes on to prompt; it
//     wraps primary's failure too.
//   - Set writes primary and then removes an older copy from secondary, as
//     far as secondary lets it. When primary fails, Set writes secondary
//     instead and reports success: the secret then lives in secondary,
//     which for a [FileStore] is a file that is not encrypted. A primary
//     that refused the write but still answers reads with an older secret
//     would hide the new one, since Get asks primary first; Set deletes
//     that older secret from primary. When primary does not let go of it
//     either, Set fails with an error wrapping both of primary's failures,
//     and keeps nothing in secondary.
//   - Delete removes the account from both, so a secret written to the
//     secondary while the primary was unavailable does not come back. When
//     primary cannot delete a secret it still returns, Delete fails with an
//     error that does not wrap [ErrNotFound], even if secondary removed its
//     copy: the next Get would return that secret again. A primary that
//     fails and cannot be read either is taken to be unavailable, as in Get
//     and Set: Delete then succeeds when secondary removed the secret, and
//     returns an error wrapping [ErrNotFound] and primary's failure when
//     secondary holds none.
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
		// Primary is taken to be unavailable, as in Set and Delete; the
		// secret is simply not stored yet.
		return "", errors.Join(secondaryErr, err)
	}
	return "", errors.Join(err, secondaryErr)
}

func (f fallback) Set(account, value string) error {
	if value == "" {
		return ErrEmpty
	}
	err := f.primary.Set(account, value)
	if err == nil {
		// A copy from a run without primary must not outlive the move: it
		// would stay readable in secondary, and come back whenever primary
		// is away.
		_ = f.secondary.Delete(account)
		return nil
	}
	if secondaryErr := f.secondary.Set(account, value); secondaryErr != nil {
		return errors.Join(err, secondaryErr)
	}
	// Get asks primary first. A primary that cannot be read is unavailable
	// and hides nothing; one that still answers with an older secret does.
	if !f.primaryHidesNew(account, value) {
		return nil
	}
	deleteErr := f.primary.Delete(account)
	if deleteErr == nil || errors.Is(deleteErr, ErrNotFound) {
		return nil
	}
	_ = f.secondary.Delete(account)
	return fmt.Errorf("secret: the primary store took no new secret and keeps the old one: %w",
		errors.Join(err, deleteErr))
}

// primaryHidesNew reports whether primary answers a read of account with a
// secret other than value, which Get would return in place of value.
func (f fallback) primaryHidesNew(account, value string) bool {
	old, err := f.primary.Get(account)
	return err == nil && old != value
}

func (f fallback) Delete(account string) error {
	err := f.primary.Delete(account)
	secondaryErr := f.secondary.Delete(account)
	if err != nil && !errors.Is(err, ErrNotFound) {
		_, getErr := f.primary.Get(account)
		switch {
		case getErr == nil:
			// Primary still returns the secret, so it is not forgotten,
			// whatever secondary did.
			if secondaryErr != nil && !errors.Is(secondaryErr, ErrNotFound) {
				err = errors.Join(err, secondaryErr)
			}
			return fmt.Errorf("secret: the primary store still holds the secret: %w", err)
		case errors.Is(getErr, ErrNotFound):
			err = getErr
		case secondaryErr == nil:
			// A primary that fails is taken to be unavailable, as in Get and Set.
			return nil
		case errors.Is(secondaryErr, ErrNotFound):
			return errors.Join(secondaryErr, err)
		default:
			return errors.Join(err, secondaryErr)
		}
	}
	// Primary removed the secret or holds none.
	switch {
	case err == nil || secondaryErr == nil:
		return nil
	case errors.Is(secondaryErr, ErrNotFound):
		return ErrNotFound
	default:
		return secondaryErr
	}
}

// Migrating returns a Store that works like Fallback(primary, secondary) and
// also moves secrets from secondary into primary. It suits a tool that kept
// its secrets in files before it used a keyring, and a keyring that was
// missing when a secret was saved:
//
//   - Get reads primary. When primary answers that it holds no secret for
//     the account, Get reads secondary, stores the value in primary and only
//     then deletes it from secondary. When primary does not take the value,
//     the secondary copy stays where it is. Failing to delete the copy is
//     not an error, since primary is read first from then on.
//   - When primary fails for another reason, such as a missing keyring,
//     secondary is the store: Get returns what secondary returns, its
//     errors alone, and moves nothing.
//   - Set and Delete are those of Fallback. Set removes the secondary's
//     copy once primary took the secret, so a secret saved again does not
//     stay behind in a file.
func Migrating(primary, secondary Store) Store {
	return migrating{fallback{primary: primary, secondary: secondary}}
}

type migrating struct{ fallback }

func (m migrating) Get(account string) (string, error) {
	value, err := m.primary.Get(account)
	switch {
	case err == nil:
		return value, nil
	case !errors.Is(err, ErrNotFound):
		return m.secondary.Get(account)
	}
	value, err = m.secondary.Get(account)
	if err != nil {
		return "", err
	}
	if m.primary.Set(account, value) == nil {
		_ = m.secondary.Delete(account)
	}
	return value, nil
}

// ErrUnavailable is what a store from [Unavailable] fails with when it was
// given no error of its own.
var ErrUnavailable = errors.New("secret: store unavailable")

// Unavailable returns a Store that is not there: every method fails with
// err, or with [ErrUnavailable] when err is nil. Use it in place of a store
// that could not be set up, such as a [FileStore] whose directory could not
// be located: a [Fallback] in front of it moves on, and err is reported once
// the store is needed. An err that wraps [ErrNotFound] makes the store look
// empty rather than missing.
func Unavailable(err error) Store {
	if err == nil {
		err = ErrUnavailable
	}
	return unavailable{err: err}
}

type unavailable struct{ err error }

func (u unavailable) Get(string) (string, error) { return "", u.err }
func (u unavailable) Set(string, string) error   { return u.err }
func (u unavailable) Delete(string) error        { return u.err }
