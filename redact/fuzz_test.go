package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

// No occurrence of a secret survives Values. A secret that is part of the
// marker, or that holds one of its brackets and so can line up with a
// marker and the text beside it, is left out: the marker itself is not
// what the check is about.
func FuzzValues(f *testing.F) {
	f.Add("token=abc123 again abc123", "abc123")
	f.Add("-aaa-", "aa")
	f.Add(`{"password":"p@ss w\/rd&"}`, "p@ss w/rd&")
	f.Add("?pw=%25%FF", "%\xff")
	f.Fuzz(func(t *testing.T, text, secret string) {
		got := Values(text, secret)
		if secret == "" || strings.Contains(Marker, secret) || strings.ContainsAny(secret, "[]") {
			return
		}
		if strings.Contains(got, secret) {
			t.Fatalf("Values(%q, %q) = %q, still holds the secret", text, secret, got)
		}
	})
}

// Patterns never turns valid JSON into something else, as its
// documentation promises.
func FuzzPatternsKeepsJSONValid(f *testing.F) {
	for _, seed := range []string{
		`{"api_key":"sk-secretvalue","n":1}`,
		`{"token": 12345, "ok": true}`,
		`{"message":"token=abc def"}`,
		`{"body":"{\"password\":\"x y\"}"}`,
		`{"token":["abc]secret","d}e\"]"],"n":1}`,
		`{"m":"token: [\"a]\", \"b\"] rest"}`,
		"{\"password\":\n  \"hunter2\"}",
		`["auth=Basic dXNlcjpwYXNz", "secret: 'x'"]`,
		"password: |\n  a\n  b\nnext: 1",
		"Authorization: Bearer abcdefgh.ijklmnop",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got := Patterns(in)
		if json.Valid([]byte(in)) && !json.Valid([]byte(got)) {
			t.Fatalf("Patterns(%q) = %q, not valid JSON", in, got)
		}
	})
}
