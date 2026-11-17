package redact

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestValues(t *testing.T) {
	got := Values("token=abc123 again abc123 and abc", "abc", "abc123", "")
	want := "token=[REDACTED] again [REDACTED] and [REDACTED]"
	if got != want {
		t.Fatalf("Values = %q, want %q", got, want)
	}
	for _, tc := range []struct {
		name    string
		text    string
		secrets []string
		want    string
	}{
		{"no secrets", "plain text", nil, "plain text"},
		{"nothing found", "plain text", []string{"hunter2"}, "plain text"},
		{"only empty secrets", "plain text", []string{"", ""}, "plain text"},
		{"overlapping secrets", "abcdef", []string{"abcd", "cdef"}, "[REDACTED]"},
		{"overlap with the longer one behind", "xabcdef!", []string{"bcdef", "abc"}, "x[REDACTED]!"},
		{"a secret overlapping itself", "-aaa-", []string{"aa"}, "-[REDACTED]-"},
		{"neighbors stay apart", "abcabc", []string{"abc"}, "[REDACTED][REDACTED]"},
		{"the marker is never matched", "token=longsecret", []string{"longsecret", "RED", "[", "E"}, "token=[REDACTED]"},
		{"regular expression characters", "a.*b and axxb", []string{".*"}, "a[REDACTED]b and axxb"},
		{"at both ends", "pw and pw", []string{"pw"}, "[REDACTED] and [REDACTED]"},
	} {
		if got := Values(tc.text, tc.secrets...); got != tc.want {
			t.Errorf("%s: Values(%q, %q) = %q, want %q", tc.name, tc.text, tc.secrets, got, tc.want)
		}
	}
}

// A secret is also found in the forms it takes inside a request.
func TestValuesEscapedForms(t *testing.T) {
	const tricky = `p@ss w/rd"<1>&ü`
	body, err := json.Marshal(map[string]string{"password": tricky})
	if err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	enc := json.NewEncoder(&plain)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]string{"password": tricky}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, text, want string }{
		{"as it is", "password: " + tricky + ".", "password: [REDACTED]."},
		{"form body", "user=alice&password=" + url.QueryEscape(tricky) + "&x=1", "user=alice&password=[REDACTED]&x=1"},
		{"query with %20", "?password=" + strings.ReplaceAll(url.QueryEscape(tricky), "+", "%20") + "&x=1", "?password=[REDACTED]&x=1"},
		{"URL path", "GET /keys/" + url.PathEscape(tricky) + "/info", "GET /keys/[REDACTED]/info"},
		{"JSON", string(body), `{"password":"[REDACTED]"}`},
		{"JSON without HTML escapes", strings.TrimSpace(plain.String()), `{"password":"[REDACTED]"}`},
		{"lower-case URL escapes", "?password=" + strings.ToLower(url.QueryEscape(tricky)) + "&x=1", "?password=[REDACTED]&x=1"},
	} {
		got := Values(tc.text, tricky)
		if got != tc.want {
			t.Errorf("%s: Values(%q) = %q, want %q", tc.name, tc.text, got, tc.want)
		}
	}
	// A secret that is not UTF-8 is matched byte for byte, its URL escapes
	// in either case.
	if got := Values("pw=%\xff and pw=%25%FF and %25%ff", "%\xff"); got != "pw=[REDACTED] and pw=[REDACTED] and [REDACTED]" {
		t.Errorf("Values of a non-UTF-8 secret = %q", got)
	}

	// A quote at the end of a secret stays part of its JSON form.
	if got := Values(`{"password":"say \"hi\""}`, `say "hi"`); got != `{"password":"[REDACTED]"}` {
		t.Errorf("Values of a quoted secret in JSON = %q", got)
	}

	// What Values cannot find, as documented: a secret inside a larger
	// encoded value.
	basic := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("alice:"+tricky))
	if got := Values(basic, tricky); got != basic {
		t.Errorf("Values found the password inside Basic credentials: %q; update its documentation", got)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte("alice:" + tricky))
	if got := Values(basic, tricky, encoded); got != "Authorization: Basic [REDACTED]" {
		t.Errorf("Values with the encoded credentials as a secret = %q", got)
	}
}
