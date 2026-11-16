package secret

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactPatterns(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"json api key", `{"api_key":"sk-secretvalue"}`, `{"api_key":"[REDACTED]"}`},
		{"yaml token", "github_token: ghp_abcdefghijk", `github_token: [REDACTED]`},
		{"env password", "DB_PASSWORD=hunter123", `DB_PASSWORD=[REDACTED]`},
		{"single-quoted value", "secret: 'a b c'", `secret: '[REDACTED]'`},
		{"escaped quote in value", `{"password":"say \"hi\""}`, `{"password":"[REDACTED]"}`},
		{"bearer token", "Authorization header: Bearer abcdefgh.ijklmnop", "Authorization header: Bearer [REDACTED]"},
		{"authorization header", "Authorization: Bearer abcdefgh.ijklmnop", `Authorization: [REDACTED]`},
		{"basic authorization header", "authorization: Basic dXNlcjpwYXNzd29yZA==", `authorization: [REDACTED]`},
		{"auth scheme alone", "auth=Basic", `auth=[REDACTED]`},
		{"short bearer word", "a Bearer of news", "a Bearer of news"},
		{"openai token", "request failed for sk-abcdefghijklmnop", "request failed for [REDACTED]"},
		{"github token", "push with ghs_abcdefghijk failed", "push with [REDACTED] failed"},
		{"gitlab token", "request failed for glpat-abcdefghijklmnop", "request failed for [REDACTED]"},
		{"aws access key", "request failed for AKIAABCDEFGHIJKLMNOP", "request failed for [REDACTED]"},
		{"mixed-case prefixed tokens", "SK-ABCDEFGHIJK GHP_ABCDEFGHIJK GlPaT-AbCdEfGhIjKl", "[REDACTED] [REDACTED] [REDACTED]"},
		{"short prefixed word", "sk-short and ghp_x", "sk-short and ghp_x"},
		{"ampersand in unquoted value", "api_key=abc&def", `api_key=[REDACTED]`},
		{"query-like unquoted value", "api_key=sk-abcdefgh&session=tok-secret", `api_key=[REDACTED]`},
		{"multiple credentials", `api_key="sk-abcdefgh" and token=ghp_abcdefghijk`, `api_key="[REDACTED]" and token=[REDACTED]`},
		{"newline-separated credentials", "password=hunter123\naccess_token=secret456", "password=[REDACTED]\naccess_token=[REDACTED]"},
		{"value ends at a comma", "user=bob, password=hunter2, ok", `user=bob, password=[REDACTED], ok`},
		{"keeps unicode around credential", "世界 api_key=secretvalue τέλος", `世界 api_key=[REDACTED] τέλος`},
		{"json number value", `{"token": 12345, "n": 1}`, `{"token": "[REDACTED]", "n": 1}`},
		{"assignment inside a json string", `{"message":"token=abc def"}`, `{"message":"token=[REDACTED] def"}`},
		{"assignment ending a json string", `{"message":"token=abc"}`, `{"message":"token=[REDACTED]"}`},
		{"escaped json inside a json string", `{"body":"{\"password\":\"x y\"}"}`, `{"body":"{\"password\":\"[REDACTED]\"}"}`},
		{"escaped quote in escaped json", `{"body":"{\"password\":\"say \\\"hi\\\" secret-tail\"}"}`, `{"body":"{\"password\":\"[REDACTED]\"}"}`},
		{"array value", `{"token":["abc","def"],"n":1}`, `{"token":"[REDACTED]","n":1}`},
		{"object value", `{"auth":{"user":"u","pass":"p"}}`, `{"auth":"[REDACTED]"}`},
		{"nested value", `{"auth":{"basic":{"password":"p"}},"n":1}`, `{"auth":"[REDACTED]","n":1}`},
		{"brackets inside strings of an array", `{"token":["abc]secret","d}e\"]"],"n":1}`, `{"token":"[REDACTED]","n":1}`},
		{"array never closed", `token: [abc, "def`, `token: [REDACTED]`},
		{"yaml literal block", "password: |\n  hunter2\n  more\nuser: bob\n", "password: [REDACTED]\nuser: bob\n"},
		{"yaml folded block", "db:\n  secret: >-\n    a b\n\n    c\n  host: x\n", "db:\n  secret: [REDACTED]\n  host: x\n"},
		{"yaml block at the end", "token: |", "token: [REDACTED]"},
		{"yaml doubled quote", "password: 'hunter''secret' # x", "password: '[REDACTED]' # x"},
		{"unclosed escaped value in a json string", `{"m":"password=\"unterminated secret tail"}`, `{"m":"password=\"[REDACTED]\""}`},
		{"no value", "password: ", "password: "},
		{"yaml anchor above the value", "password: &db\n  hunter2\nuser: bob", "password: \n  [REDACTED]\nuser: bob"},
		{"yaml tag above the value", "password: !!str  \n  hunter2\nuser: bob", "password: \n  [REDACTED]\nuser: bob"},
		{"yaml comment above the value", "password: # old one\n  hunter2\nuser: bob", "password: # old one\n  [REDACTED]\nuser: bob"},
		{"yaml tag and comment above the value", "password: !!str # c\n  hunter2", "password: # c\n  [REDACTED]"},
		{"yaml comment without a value", "password: # none\nuser: bob", "password: # none\nuser: bob"},
		{"hash inside a value is no comment", "password=#abc def", "password=[REDACTED]"},
		{"yaml flow mapping with a stray quote", `{note: 5" screen, password: "hunter2"}`, `{note: 5" screen, password: "[REDACTED]"}`},
		{"json value on the next line", "{\"password\":\n  \"hunter2\"}", "{\"password\":\n  \"[REDACTED]\"}"},
		{"json number on the next line", "{\"token\":\n 123}", "{\"token\":\n \"[REDACTED]\"}"},
		{"json negative number on the next line", "{\"token\":\n -5}", "{\"token\":\n \"[REDACTED]\"}"},
		{"yaml sequence under a quoted key", "\"password\":\n  - hunter2\n  - secret-tail\nuser: bob\n", "\"password\":\n  [REDACTED]\nuser: bob\n"},
		{"yaml verbatim tag", "password: !<tag:yaml.org,2002:str> hunter2\nuser: bob", "password: [REDACTED]\nuser: bob"},
		{"yaml indentless sequence", "password:\n- hunter2\n- secret-tail\nuser: bob", "password:\n[REDACTED]\nuser: bob"},
		{"yaml multiline scalar under a quoted key", "\"password\":\n  123\n  456\nuser: bob", "\"password\":\n  [REDACTED]\nuser: bob"},
		{"yaml plain scalar with spaces", "password: first part\nuser: bob", "password: [REDACTED]\nuser: bob"},
		{"yaml plain scalar continued", "password: first-part\n  secret-tail\nuser: bob", "password: [REDACTED]\nuser: bob"},
		{"yaml plain scalar before a comment", "password: hunter2 # rotate soon\nuser: bob", "password: [REDACTED] # rotate soon\nuser: bob"},
		{"setting with spaces", "PASSWORD=two words\nUSER=bob", "PASSWORD=[REDACTED]\nUSER=bob"},
		{"yaml plain scalar under a quoted key", "\"password\": first secret-tail\nuser: bob", "\"password\": \"[REDACTED]\"\nuser: bob"},
		{"yaml flow mapping", "{password: first secret-tail, user: bob}", "{password: [REDACTED], user: bob}"},
		{"yaml flow mapping with a quoted key", "{\"password\": first secret-tail, user: bob}", "{\"password\": \"[REDACTED]\", user: bob}"},
		{"yaml flow scalar over lines", "{password: first\n  secret-tail, user: bob}", "{password: [REDACTED], user: bob}"},
		{"token ending in a dash", "use glpat-abcdefghijklmnop- now", "use [REDACTED] now"},
		{"token of dashes", "use glpat-abc----- now", "use [REDACTED] now"},
		{"yaml value after a blank line", "password:\n\n  hunter2\nuser: bob", "password:\n  [REDACTED]\nuser: bob"},
		{"yaml block in a sequence item", "- password: |\n    hunter2\n  user: bob", "- password: [REDACTED]\n  user: bob"},
		{"yaml value below a key in a sequence item", "- password:\n    hunter2\n  user: bob", "- password:\n    [REDACTED]\n  user: bob"},
		{"yaml nested sequence items", "- - password: first\n      secret-tail\n    user: bob", "- - password: [REDACTED]\n    user: bob"},
		{"yaml plain value under a quoted key", "\"password\":\n  hunter2\nuser: bob\n", "\"password\":\n  [REDACTED]\nuser: bob\n"},
		{"digest authorization", `Authorization: Digest username="alice", nonce="n0", response="r1"` + "\nnext", "Authorization: [REDACTED]\nnext"},
		{"digest authorization in a json string", `{"h":"Authorization: Digest username=\"a\", response=\"r\"","n":1}`, `{"h":"Authorization: [REDACTED]","n":1}`},
		{"yaml tag", "password: !!str hunter2\nuser: x", "password: [REDACTED]\nuser: x"},
		{"yaml anchor", "password: &db hunter2 # c", "password: [REDACTED] # c"},
		{"yaml tag and quotes", "password: !secret 'hunter2'", "password: '[REDACTED]'"},
		{"double quote inside yaml single quotes", "note: 'a \" quote'\npassword: \"hunter2\"", "note: 'a \" quote'\npassword: \"[REDACTED]\""},
		{"stray quote on an earlier line", "size: 5\" screen\npassword: |\n  hunter2\n", "size: 5\" screen\npassword: [REDACTED]\n"},
		{"yaml literal backslash in an array", `token: ['abc\', 'prefix]secret-tail'] # x`, `token: [REDACTED] # x`},
		{"escaping single quotes in an array", `token: ['a\'b]c', 'd'] # x`, `token: [REDACTED] # x`},
		{"no value at the end of a json string", `{"m":"password=","n":1}`, `{"m":"password=","n":1}`},
		{"yaml single quotes keep backslashes", `password: 'abc\''secret-tail' # x`, `password: '[REDACTED]' # x`},
		{"python single quotes escape", `secret='a\'b' rest`, `secret='[REDACTED]' rest`},
		{"yaml block without lines", "token: |\nuser: x", "token: [REDACTED]\nuser: x"},
		{"empty env value", "PASSWORD=\nUSER=bob\n", "PASSWORD=\nUSER=bob\n"},
		{"empty yaml value", "password:\nuser: bob\n", "password:\nuser: bob\n"},
		{"yaml value on the next line", "db:\n  password:\n    hunter2\n  user: bob\n", "db:\n  password:\n    [REDACTED]\n  user: bob\n"},
		{"escaped array with escaped quotes", `{"body":"{\"token\":[\"prefix\\\" ] secret-tail\", \"other\"]}"}`, `{"body":"{\"token\":[REDACTED]}"}`},
		{"no value before a comma", "token=, next", "token=, next"},
		{"unclosed quoted value", `password="hunter2`, `password="[REDACTED]"`},
		{"unclosed json value", `{"password":"hunter2`, `{"password":"[REDACTED]"`},
		{"unclosed single-quoted value", `secret: 'abc`, `secret: '[REDACTED]'`},
		{"array opener inside a json string", `{"message":"token: [abc","n":1}`, `{"message":"token: [REDACTED]","n":1}`},
		{"array inside a json string", `{"m":"token: [\"a]\", \"b\"] rest"}`, `{"m":"token: [REDACTED] rest"}`},
		{"yaml flow sequence", "api_token: [a, b]", "api_token: [REDACTED]"},
		{"plural name is no label", "tokens: [a, b]", "tokens: [a, b]"},
		{"escaped quotes in an unquoted value", `{"m":"password=\"x\" end"}`, `{"m":"password=\"[REDACTED]\" end"}`},
		{"keeps ordinary text", "token count: 123", "token count: 123"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RedactPatterns(tt.in); got != tt.want {
				t.Fatalf("RedactPatterns(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Redacting JSON leaves valid JSON, wherever the credential sits in it.
func TestRedactPatternsKeepsJSONValid(t *testing.T) {
	for _, in := range []string{
		`{"api_key":"sk-secretvalue","n":1}`,
		`{"token": 12345, "ok": true}`,
		`{"password": null}`,
		`{"message":"token=abc def"}`,
		`{"message":"login with password=hunter2"}`,
		`{"body":"{\"password\":\"x y\"}"}`,
		`{"m":"password=\"x\" end"}`,
		`{"body":"{\"password\":\"say \\\"hi\\\" secret-tail\"}"}`,
		`{"token":["abc","def"],"n":1}`,
		`{"auth":{"user":"u","pass":"p"}}`,
		`{"auth":{"basic":{"password":"p"}},"n":1}`,
		`{"token":["abc]secret","d}e\"]"],"n":1}`,
		`{"message":"token: [abc","n":1}`,
		`{"m":"token: [\"a]\", \"b\"] rest"}`,
		`{"m":"password=\"unterminated secret tail"}`,
		`{"body":"{\"token\":[\"prefix\\\" ] secret-tail\", \"other\"]}"}`,
		"{\"password\":\n  \"hunter2\"}",
		"{\"token\":\n 123}",
		`{"h":"Authorization: Digest username=\"a\", response=\"r\"","n":1}`,
		`{"header":"Authorization: Bearer abcdefgh.ijklmnop"}`,
		`["auth=Basic dXNlcjpwYXNz", "secret: 'x'"]`,
		`{"m":"password=\"abc\\","n":"ok"}`,
		`{"message":"token: ['abc]secret-tail', 'other']"}`,
		`{"m":"password='a''b\"secret-tail'","n":"ok"}`,
	} {
		got := RedactPatterns(in)
		if !json.Valid([]byte(got)) {
			t.Errorf("RedactPatterns(%s) = %s, not valid JSON", in, got)
		}
		if got == in {
			t.Errorf("RedactPatterns(%s) redacted nothing", in)
		}
		if strings.Contains(got, "secret-tail") {
			t.Errorf("RedactPatterns(%s) = %s, leaves part of the secret", in, got)
		}
	}
}
