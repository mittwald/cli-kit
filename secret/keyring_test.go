package secret

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	gokeyring "github.com/zalando/go-keyring"
)

// guardHelperEnv makes the test binary run TestKeyringGuardInAFreshTestBinary's
// check instead of skipping it.
const guardHelperEnv = "CLI_KIT_SECRET_GUARD_HELPER"

// A test binary that installs no mock of its own never reaches the
// operating system keyring. The check runs in a fresh copy of the test
// binary, since the other tests here install go-keyring's mock.
func TestKeyringIsOffInTestBinaries(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestKeyringGuardInAFreshTestBinary$", "-test.v")
	cmd.Env = append(os.Environ(), guardHelperEnv+"=1", TestKeyringEnv+"=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fresh test binary failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "--- PASS: TestKeyringGuardInAFreshTestBinary") {
		t.Fatalf("fresh test binary did not run the check:\n%s", out)
	}
}

func TestKeyringGuardInAFreshTestBinary(t *testing.T) {
	if os.Getenv(guardHelperEnv) == "" {
		t.Skip("runs in a fresh test binary started by TestKeyringIsOffInTestBinaries")
	}
	k := Keyring{Service: "cli-kit-test"}
	if _, err := k.Get("alice"); !errors.Is(err, ErrTestKeyring) || errors.Is(err, ErrNotFound) {
		t.Errorf("Get = %v, want ErrTestKeyring and not ErrNotFound", err)
	}
	if err := k.Set("alice", "pw"); !errors.Is(err, ErrTestKeyring) {
		t.Errorf("Set = %v, want ErrTestKeyring", err)
	}
	if err := k.Delete("alice"); !errors.Is(err, ErrTestKeyring) {
		t.Errorf("Delete = %v, want ErrTestKeyring", err)
	}
}

func TestGuardTestKeyring(t *testing.T) {
	t.Cleanup(gokeyring.MockInit)
	if guardTestKeyring(false, "") {
		t.Error("a program that is no test binary had its keyring switched off")
	}
	if guardTestKeyring(true, "system") {
		t.Errorf("a test run with %s=system had its keyring switched off", TestKeyringEnv)
	}
	if !guardTestKeyring(true, "") {
		t.Fatal("a test binary kept the operating system keyring")
	}

	// Switched off, the keyring looks missing, so a Fallback moves on.
	files := &Memory{}
	store := Fallback(Keyring{Service: "cli-kit-test"}, files)
	if err := store.Set("alice", "pw"); err != nil {
		t.Fatalf("Fallback Set = %v", err)
	}
	if got, _ := files.Get("alice"); got != "pw" {
		t.Errorf("secondary holds %q, want the value", got)
	}

	// A test that installs go-keyring's mock gets that one.
	gokeyring.MockInit()
	if err := (Keyring{Service: "cli-kit-test"}).Set("alice", "pw"); err != nil {
		t.Errorf("Set with go-keyring's mock = %v", err)
	}
}
