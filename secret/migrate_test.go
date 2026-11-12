package secret

import (
	"errors"
	"fmt"
	"testing"
)

// readOnly is a store that reads but refuses every write, like a locked
// keyring.
type readOnly struct{ Store }

var errLocked = errors.New("keyring is locked")

func (readOnly) Set(string, string) error { return errLocked }

// refusing is a store whose Get fails the way a FileStore does on a file
// others can read.
type refusing struct{ Memory }

func (*refusing) Get(account string) (string, error) {
	return "", fmt.Errorf("%w: %s is accessible by other users", ErrInsecureFile, account)
}

func TestMigratingIsAStore(t *testing.T) {
	exerciseStore(t, Migrating(&Memory{}, &Memory{}))
}

// A secret only the secondary holds moves into the primary the first time
// it is read, and leaves the secondary.
func TestMigratingMovesASecretIntoPrimary(t *testing.T) {
	primary, secondary := &Memory{}, NewMemory(map[string]string{"alice": "from-file"})
	s := Migrating(primary, secondary)
	if got, err := s.Get("alice"); err != nil || got != "from-file" {
		t.Fatalf("Get = %q, %v; want the secondary's value", got, err)
	}
	if got, err := primary.Get("alice"); err != nil || got != "from-file" {
		t.Errorf("primary holds %q, %v; want the value moved there", got, err)
	}
	if _, err := secondary.Get("alice"); !errors.Is(err, ErrNotFound) {
		t.Errorf("secondary still holds the value after the move: %v", err)
	}
	if got, err := s.Get("alice"); err != nil || got != "from-file" {
		t.Errorf("Get after the move = %q, %v", got, err)
	}
}

// What the primary holds wins, and the secondary is left alone.
func TestMigratingPrefersPrimary(t *testing.T) {
	primary := NewMemory(map[string]string{"alice": "from-keyring"})
	secondary := NewMemory(map[string]string{"alice": "from-file"})
	if got, err := Migrating(primary, secondary).Get("alice"); err != nil || got != "from-keyring" {
		t.Fatalf("Get = %q, %v; want the primary's value", got, err)
	}
	if got, _ := secondary.Get("alice"); got != "from-file" {
		t.Errorf("secondary holds %q; it was touched", got)
	}
}

// A primary that does not take the value leaves the secondary's copy in
// place: nothing is lost on the way.
func TestMigratingKeepsTheCopyWhenPrimaryRefuses(t *testing.T) {
	secondary := NewMemory(map[string]string{"alice": "from-file"})
	s := Migrating(readOnly{&Memory{}}, secondary)
	if got, err := s.Get("alice"); err != nil || got != "from-file" {
		t.Fatalf("Get = %q, %v; want the secondary's value", got, err)
	}
	if got, err := secondary.Get("alice"); err != nil || got != "from-file" {
		t.Errorf("secondary holds %q, %v; want the copy kept", got, err)
	}
}

// Without a primary, such as on a server without a keyring, the secondary
// is the store: nothing moves, and its answers are the ones reported.
func TestMigratingWithoutPrimaryUsesSecondary(t *testing.T) {
	secondary := NewMemory(map[string]string{"alice": "from-file"})
	s := Migrating(failing{}, secondary)
	if got, err := s.Get("alice"); err != nil || got != "from-file" {
		t.Fatalf("Get = %q, %v; want the secondary's value", got, err)
	}
	if got, _ := secondary.Get("alice"); got != "from-file" {
		t.Errorf("secondary holds %q; want the value kept without a primary", got)
	}
	if _, err := s.Get("bob"); !errors.Is(err, ErrNotFound) || errors.Is(err, errUnavailable) {
		t.Errorf("Get of a missing secret = %v, want ErrNotFound alone", err)
	}
	if _, err := Migrating(failing{}, &refusing{}).Get("bob"); !errors.Is(err, ErrInsecureFile) || errors.Is(err, errUnavailable) {
		t.Errorf("Get of a refused secret = %v, want the secondary's refusal alone", err)
	}
	if err := s.Set("bob", "typed"); err != nil {
		t.Fatalf("Set = %v", err)
	}
	if got, _ := secondary.Get("bob"); got != "typed" {
		t.Errorf("Set wrote %q to the secondary, want the value", got)
	}
}

// A secondary that refuses to read its copy, such as a file others can
// read, reports why; nothing is moved.
func TestMigratingReportsTheSecondaryRefusal(t *testing.T) {
	primary := &Memory{}
	if _, err := Migrating(primary, &refusing{}).Get("alice"); !errors.Is(err, ErrInsecureFile) {
		t.Fatalf("Get = %v, want ErrInsecureFile", err)
	}
	if _, err := primary.Get("alice"); !errors.Is(err, ErrNotFound) {
		t.Errorf("primary holds a value after a refused read: %v", err)
	}
}

// Delete removes the secret from both, so a copy written to the secondary
// while the primary was away does not come back.
func TestMigratingDeleteDropsBoth(t *testing.T) {
	primary := NewMemory(map[string]string{"alice": "from-keyring"})
	secondary := NewMemory(map[string]string{"alice": "from-file"})
	if err := Migrating(primary, secondary).Delete("alice"); err != nil {
		t.Fatalf("Delete = %v", err)
	}
	for name, store := range map[string]Store{"primary": primary, "secondary": secondary} {
		if _, err := store.Get("alice"); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s still holds the secret: %v", name, err)
		}
	}
}

func TestUnavailable(t *testing.T) {
	cause := errors.New("locate the home directory: $HOME is not defined")
	s := Unavailable(cause)
	if _, err := s.Get("alice"); !errors.Is(err, cause) || errors.Is(err, ErrNotFound) {
		t.Errorf("Get = %v, want the cause and not ErrNotFound", err)
	}
	if err := s.Set("alice", "pw"); !errors.Is(err, cause) {
		t.Errorf("Set = %v, want the cause", err)
	}
	if err := s.Delete("alice"); !errors.Is(err, cause) {
		t.Errorf("Delete = %v, want the cause", err)
	}
	if _, err := Unavailable(nil).Get("alice"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Get without a cause = %v, want ErrUnavailable", err)
	}

	// Behind a working primary it is never needed.
	primary := &Memory{}
	store := Fallback(primary, s)
	if err := store.Set("alice", "pw"); err != nil {
		t.Fatalf("Fallback Set = %v", err)
	}
	if got, err := store.Get("alice"); err != nil || got != "pw" {
		t.Errorf("Fallback Get = %q, %v", got, err)
	}
}
