package secret_test

import (
	"errors"
	"fmt"
	"os"

	"github.com/mittwald/cli-kit/secret"
)

// A token is looked up in $EXAMPLE_TOKEN, then in the keyring (or, without
// one, in a file under the user's configuration directory), and asked for
// interactively as a last resort. It is saved only after the service
// accepted it, and dropped again when the service rejects it.
func ExampleResolver() {
	files, err := secret.NewFileStore("example-tool")
	if err != nil {
		fmt.Println(err)
		return
	}
	r := &secret.Resolver{
		Label:     "API token",
		Account:   "default",
		Store:     secret.Fallback(secret.Keyring{Service: "example-tool"}, files),
		Env:       "EXAMPLE_TOKEN",
		Hint:      "Create a token at https://example.com/settings/tokens.",
		TrimSpace: true,
	}

	call := func(token string) error { return nil } // a request to the service
	for range 2 {
		token, source, err := r.Resolve()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		err = call(token)
		if err == nil {
			_ = r.Save(token)
			return
		}
		if forgetErr := r.Forget(source); forgetErr != nil {
			fmt.Fprintln(os.Stderr, forgetErr)
			return
		}
	}
}

func ExampleRedact() {
	token := "glpat-abc123"
	fmt.Println(secret.Redact("GET /api?private_token=glpat-abc123", token))
	// Output: GET /api?private_token=[REDACTED]
}

func ExampleFallback() {
	keyring := secret.NewMemory(nil) // stands in for secret.Keyring
	files := &secret.Memory{}        // stands in for a *secret.FileStore
	store := secret.Fallback(keyring, files)
	_ = store.Set("default", "hunter2")
	value, err := store.Get("default")
	fmt.Println(value, errors.Is(err, secret.ErrNotFound))
	// Output: hunter2 false
}
