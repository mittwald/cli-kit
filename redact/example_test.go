package redact_test

import (
	"fmt"

	"github.com/mittwald/cli-kit/redact"
)

func ExampleValues() {
	token := "glpat-abc123"
	fmt.Println(redact.Values("GET /api?private_token=glpat-abc123", token))
	// Output: GET /api?private_token=[REDACTED]
}

func ExamplePatterns() {
	fmt.Println(redact.Patterns(`{"user":"alice","password":"hunter2"}`))
	fmt.Println(redact.Patterns("https://example.com/hook?token=abc123&page=2"))
	// Output:
	// {"user":"alice","password":"[REDACTED]"}
	// https://example.com/hook?token=[REDACTED]
}
