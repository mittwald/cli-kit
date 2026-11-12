package format

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/goccy/go-yaml"
)

// edgeCase carries one of every scalar a YAML reader could mistake for
// something other than the string it is.
type edgeCase struct {
	Plain    string   `json:"plain"`
	Comma    string   `json:"comma"`
	Colon    string   `json:"colon"`
	Quote    string   `json:"quote"`
	Hash     string   `json:"hash"`
	Trail    string   `json:"trail"`
	Empty    string   `json:"empty"`
	Numlike  string   `json:"numlike"`
	Datelike string   `json:"datelike"`
	Boolish  string   `json:"boolish"`
	Tilde    string   `json:"tilde"`
	Multi    string   `json:"multi"`
	Dash     string   `json:"dash"`
	List     []string `json:"list"`
	Nil      *string  `json:"nil"`
	Num      float64  `json:"num"`
	Yes      bool     `json:"yes"`
	Skipped  string   `json:"skipped,omitempty"`
}

func sampleEdgeCase() edgeCase {
	return edgeCase{
		Plain: "Ada Lovelace", Comma: "first, second", Colon: "09:04 - 13:24",
		Quote: `say "hello"`, Hash: "#1 issue", Trail: "trailing  ", Empty: "",
		Numlike: "0815", Datelike: "2026-06-16", Boolish: "true", Tilde: "~",
		Multi: "line one\nline two", Dash: "- dash", List: []string{"a", "b"},
		Num: 12.5, Yes: true,
	}
}

// Every scalar whose plain form would come back as another type — a time, a
// date, a boolean, a number — is quoted, and the rest stays readable. A digit
// string with a leading zero (0815) is quoted too: it is not valid octal, so
// YAML 1.1 readers take it for a string, but a YAML 1.2 reader takes it for
// the number 815.
func TestYAMLQuotesAmbiguousScalars(t *testing.T) {
	var out bytes.Buffer
	if err := WriteYAML(&out, sampleEdgeCase()); err != nil {
		t.Fatalf("WriteYAML returned error: %v", err)
	}
	want := `plain: Ada Lovelace
comma: first, second
colon: "09:04 - 13:24"
quote: say "hello"
hash: "#1 issue"
trail: "trailing  "
empty: ""
numlike: "0815"
datelike: "2026-06-16"
boolish: "true"
tilde: "~"
multi: |-
  line one
  line two
dash: "- dash"
list:
  - a
  - b
nil: null
num: 12.5
"yes": true
`
	if got := out.String(); got != want {
		t.Fatalf("yaml mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// An identifier that is all digits has to come back as the string it is.
func TestYAMLQuotesDigitStrings(t *testing.T) {
	var out bytes.Buffer
	if err := WriteYAML(&out, map[string]string{"id": "4711"}); err != nil {
		t.Fatalf("WriteYAML returned error: %v", err)
	}
	if got := out.String(); got != "id: \"4711\"\n" {
		t.Fatalf("digit string not quoted: %q", got)
	}
}

// ambiguousScalars are strings some YAML reader takes for something other
// than a string when they are written plain: YAML 1.1 readers (PyYAML, Ruby)
// and YAML 1.2 ones (go-yaml, yq, js-yaml) disagree on many of them, so each
// has to be quoted for all of them to agree.
var ambiguousScalars = []string{
	// Decimal integers: signed, with leading zeros (a float for go-yaml,
	// octal for YAML 1.1), and grouped by underscores or commas.
	"0", "4711", "+1", "-1", "0815", "08", "09111", "007", "1_000", "1,000", "80,443",
	// Hexadecimal, octal and binary integers. PyYAML fails on "0x_".
	"0x1F", "0X1f", "0x_", "0o17", "017", "0b11", "-0b1",
	// Floats: YAML 1.2 takes an exponent without a dot, YAML 1.1 does not.
	"1.5", ".5", "1.", "-.5", "+1.5e3", "1e3", "1E5", "12345e7", "0e0", "1e-7", "1.0e+21", "6.02E23",
	".inf", "-.Inf", "+.INF", ".nan", ".NaN", ".NAN",
	// Base 60 integers and floats, which YAML 1.1 reads times of day as.
	"12:30:45", "1:30", "+1:30", "-1:30", "0:00:05", "190:20:30", "1:2:3.5", "1:0.",
	// Dates and time stamps.
	"2026-06-16", "2026-6-1", "2026-06-16T10:00:00Z", "2026-06-16 10:00:00",
	"2026-06-16t10:00:00+02:00", "2026-6-1 9:00",
	// Booleans and null, the YAML 1.1 spellings included.
	"true", "True", "TRUE", "false", "False", "FALSE", "yes", "Yes", "YES", "no", "No", "NO",
	"on", "On", "ON", "off", "Off", "OFF", "y", "Y", "n", "N", "null", "Null", "NULL", "~",
	// Structure: indicators, comments, and the YAML 1.1 merge and value keys.
	"?", "? x", "-", "- x", "<<", "=", "", " x", "x ", "x: y", "x:", "x #y", "#x",
	"[x", "{x", "*x", "&x", "!x", "|", ">", "%x", "@x", "`x", "'x", `"x`,
}

// plainScalars are strings no reader takes for anything else, which stay
// readable: versions, addresses and words that only resemble a number.
var plainScalars = []string{
	"Ada Lovelace", "0.1.0", "1.2.3", "10.0.0.1", "v1", "1e", "e3", "0x", "0b2", "0o8",
	".", "..", "+", "x1", "1-2", "1h30m", "abc123e4", "_1", "2026-06", "20:30h",
	"?x", "-x", "a?b", "first, second", `say "hello"`, "it's", "a/b", "a=b", "<a&b>",
}

// A string a reader of either YAML version could take for a number, a time,
// a boolean, null or structure is double-quoted wherever it stands, and
// reads back as the string it is.
func TestYAMLQuotesWhatReadersTakeForSomethingElse(t *testing.T) {
	for _, s := range ambiguousScalars {
		quoted := strconv.Quote(s)
		for _, tc := range []struct {
			place string
			view  any
			want  string
		}{
			{"value", map[string]string{"v": s}, "v: " + quoted + "\n"},
			{"item", []string{s}, "  - " + quoted + "\n"},
			{"key", map[string]int{s: 1}, quoted + ": 1\n"},
			{"root", s, quoted + "\n"},
		} {
			var out bytes.Buffer
			if err := WriteYAML(&out, tc.view); err != nil {
				t.Fatalf("WriteYAML(%q as %s) returned error: %v", s, tc.place, err)
			}
			if got := out.String(); got != tc.want {
				t.Errorf("WriteYAML(%q as %s) = %q, want %q", s, tc.place, got, tc.want)
			}
		}
	}
	var out bytes.Buffer
	if err := WriteYAML(&out, ambiguousScalars); err != nil {
		t.Fatalf("WriteYAML returned error: %v", err)
	}
	var back []string
	if err := yaml.Unmarshal(out.Bytes(), &back); err != nil || !reflect.DeepEqual(back, ambiguousScalars) {
		t.Errorf("the strings read back as %q, %v", back, err)
	}
}

// Quoting stops at what a reader could misread: a version, an address or a
// word with digits in it stays plain.
func TestYAMLLeavesUnambiguousScalarsPlain(t *testing.T) {
	for _, s := range plainScalars {
		var out bytes.Buffer
		if err := WriteYAML(&out, map[string]string{"v": s}); err != nil {
			t.Fatalf("WriteYAML(%q) returned error: %v", s, err)
		}
		if got, want := out.String(), "v: "+s+"\n"; got != want {
			t.Errorf("WriteYAML(%q) = %q, want %q", s, got, want)
		}
		var back map[string]string
		if err := yaml.Unmarshal(out.Bytes(), &back); err != nil || back["v"] != s {
			t.Errorf("WriteYAML(%q) reads back as %q, %v", s, back["v"], err)
		}
	}
}

// A number is written with the digits JSON has and reads back as a number
// of the same kind: an integer however large, and a float with a dot in its
// mantissa and a sign in its exponent, which YAML 1.1 needs to see a float.
func TestYAMLKeepsJSONNumbers(t *testing.T) {
	for _, tc := range []struct{ json, yaml string }{
		{"0", "0"},
		{"-0", "-0"},
		{"-12", "-12"},
		{"18446744073709551615", "18446744073709551615"},
		{"18446744073709551616", "18446744073709551616"},
		{"-9223372036854775809", "-9223372036854775809"},
		{"123456789012345678901234567890", "123456789012345678901234567890"},
		{"0.3", "0.3"},
		{"2.50", "2.50"},
		{"1e-7", "1.0e-7"},
		{"1e+21", "1.0e+21"},
		{"1E5", "1.0E+5"},
		{"1.5e3", "1.5e+3"},
		{"2.5E-10", "2.5E-10"},
		{
			"1234567890123456789012345678901234567890.123456789012345678901234567890",
			"1234567890123456789012345678901234567890.123456789012345678901234567890",
		},
	} {
		var out bytes.Buffer
		if err := WriteYAML(&out, map[string]json.RawMessage{"v": json.RawMessage(tc.json)}); err != nil {
			t.Fatalf("WriteYAML(%s) returned error: %v", tc.json, err)
		}
		if got, want := out.String(), "v: "+tc.yaml+"\n"; got != want {
			t.Errorf("WriteYAML(%s) = %q, want %q", tc.json, got, want)
		}
	}

	// The same for the numbers encoding/json writes for Go values.
	huge, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	view := struct {
		Small  float64     `json:"small"`
		Large  float64     `json:"large"`
		Whole  float64     `json:"whole"`
		Max    uint64      `json:"max"`
		Huge   *big.Int    `json:"huge"`
		Number json.Number `json:"number"`
	}{1e-7, 1e21, 1e20, math.MaxUint64, huge, "12345678901234567890.123456789"}
	var out bytes.Buffer
	if err := WriteYAML(&out, view); err != nil {
		t.Fatalf("WriteYAML returned error: %v", err)
	}
	want := `small: 1.0e-7
large: 1.0e+21
whole: 100000000000000000000
max: 18446744073709551615
huge: 123456789012345678901234567890
number: 12345678901234567890.123456789
`
	if got := out.String(); got != want {
		t.Fatalf("yaml mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
	var back struct {
		Small, Large float64
		Max          uint64
	}
	if err := yaml.Unmarshal(out.Bytes(), &back); err != nil || back.Small != 1e-7 || back.Large != 1e21 || back.Max != math.MaxUint64 {
		t.Errorf("the numbers read back as %+v, %v", back, err)
	}
}

// repeated is a value whose JSON holds a key twice, which only a custom
// MarshalJSON can write.
type repeated struct{}

func (repeated) MarshalJSON() ([]byte, error) {
	return []byte(`{"a":1,"b":2,"a":{"x":1,"x":2},"c":[{"d":1,"d":null}]}`), nil
}

// WriteJSON writes an object that repeats a key as it is. WriteYAML, where
// readers reject a repeated key, writes the key once: in its first place and
// with its last value, which is how JSON readers take the object.
func TestYAMLWritesRepeatedKeysOnce(t *testing.T) {
	var j, y bytes.Buffer
	if err := WriteJSON(&j, repeated{}); err != nil {
		t.Fatalf("WriteJSON returned error: %v", err)
	}
	if err := WriteYAML(&y, repeated{}); err != nil {
		t.Fatalf("WriteYAML returned error: %v", err)
	}
	if got, want := y.String(), "a:\n  x: 2\nb: 2\nc:\n  - d: null\n"; got != want {
		t.Fatalf("WriteYAML = %q, want %q", got, want)
	}
	var fromJSON, fromYAML map[string]any
	if err := json.Unmarshal(j.Bytes(), &fromJSON); err != nil {
		t.Fatalf("re-reading the JSON failed: %v", err)
	}
	if err := yaml.Unmarshal(y.Bytes(), &fromYAML); err != nil {
		t.Fatalf("re-reading the YAML failed: %v", err)
	}
	if normalized := viaJSON(t, fromYAML); !reflect.DeepEqual(normalized, fromJSON) {
		t.Errorf("YAML reads back as %v, JSON as %v", normalized, fromJSON)
	}
}

// viaJSON returns v as encoding/json decodes its JSON encoding, so numbers
// compare whatever type a YAML reader gave them.
func viaJSON(t *testing.T, v any) any {
	t.Helper()
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	return decoded
}

// A key holding "<<" is quoted, since goccy/go-yaml does not read it plain,
// and a key longer than the 1024 characters readers allow before the colon
// is written as an explicit key.
func TestYAMLWritesKeysEveryReaderTakes(t *testing.T) {
	long := strings.Repeat("k", 1500)
	for _, tc := range []struct {
		name string
		view any
		want string
	}{
		{"merge", map[string]int{"0<<": 1}, "\"0<<\": 1\n"},
		{"merge inside", map[string]int{"a<<b": 1}, "\"a<<b\": 1\n"},
		{"merge alone", map[string]int{"<<": 1}, "\"<<\": 1\n"},
		{"shift value", map[string]string{"v": "1<<2"}, "v: 1<<2\n"},
		{"short", map[string]int{strings.Repeat("k", 900): 1}, strings.Repeat("k", 900) + ": 1\n"},
		{"long", map[string]int{long: 1, "z": 2}, "? " + long + "\n: 1\nz: 2\n"},
		{"long quoted", map[string]int{long + ": x": 1}, "? \"" + long + ": x\"\n: 1\n"},
		{
			"long nested", map[string]any{"a": []any{map[string]any{long: []int{1}, "z": map[string]any{long: "v"}}}},
			"a:\n  - ? " + long + "\n    :\n      - 1\n    z:\n      ? " + long + "\n      : v\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := WriteYAML(&out, tc.view); err != nil {
				t.Fatalf("WriteYAML returned error: %v", err)
			}
			if got := out.String(); got != tc.want {
				t.Fatalf("WriteYAML = %q, want %q", got, tc.want)
			}
			var back any
			if err := yaml.Unmarshal(out.Bytes(), &back); err != nil {
				t.Fatalf("goccy/go-yaml does not read the output: %v", err)
			}
			want := viaJSON(t, tc.view)
			if got := viaJSON(t, back); !reflect.DeepEqual(got, want) {
				t.Errorf("goccy/go-yaml reads %v, want %v", got, want)
			}
			if parse := pythonYAML(); parse != nil {
				if got, err := parse(out.Bytes()); err != nil || !reflect.DeepEqual(got, want) {
					t.Errorf("PyYAML reads %v, %v, want %v", got, err, want)
				}
			}
		})
	}
}

type duration time.Duration

func (d duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

type nested struct {
	Name  string     `json:"name"`
	Items []item     `json:"items"`
	Took  duration   `json:"took"`
	When  *time.Time `json:"when,omitempty"`
}

type item struct {
	Zeta  string `json:"zeta"`
	Alpha int    `json:"alpha"`
}

// Field order follows the struct, custom marshalers apply, and a sequence of
// mappings keeps the dash on the first key with the block two columns in.
func TestYAMLFollowsJSONView(t *testing.T) {
	var out bytes.Buffer
	v := nested{Name: "build", Items: []item{{"z", 1}, {"y", 2}}, Took: duration(90 * time.Second)}
	if err := WriteYAML(&out, v); err != nil {
		t.Fatalf("WriteYAML returned error: %v", err)
	}
	want := `name: build
items:
  - zeta: z
    alpha: 1
  - zeta: "y"
    alpha: 2
took: 1m30s
`
	if got := out.String(); got != want {
		t.Fatalf("yaml mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestWriteJSONIndentsAndKeepsHTMLCharacters(t *testing.T) {
	var out bytes.Buffer
	if err := WriteJSON(&out, map[string]string{"arrow": "a -> b & <c>"}); err != nil {
		t.Fatalf("WriteJSON returned error: %v", err)
	}
	want := "{\n  \"arrow\": \"a -> b & <c>\"\n}\n"
	if got := out.String(); got != want {
		t.Fatalf("json mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestWriteDispatchesStructuredFormats(t *testing.T) {
	v := map[string]int{"count": 1}
	var j, y bytes.Buffer
	if err := Write(&j, JSON, v); err != nil || j.String() != "{\n  \"count\": 1\n}\n" {
		t.Fatalf("Write(JSON) = %q, %v", j.String(), err)
	}
	if err := Write(&y, YAML, v); err != nil || y.String() != "count: 1\n" {
		t.Fatalf("Write(YAML) = %q, %v", y.String(), err)
	}
	for _, f := range []Format{Text, CSV, Markdown} {
		if err := Write(&bytes.Buffer{}, f, v); !errors.Is(err, ErrNotStructured) {
			t.Fatalf("Write(%s) error = %v, want ErrNotStructured", f, err)
		}
	}
}

func TestWriteJSONReportsMarshalErrors(t *testing.T) {
	if err := WriteJSON(&bytes.Buffer{}, map[string]any{"c": make(chan int)}); err == nil {
		t.Fatal("WriteJSON accepted an unmarshalable value")
	}
	if err := WriteYAML(&bytes.Buffer{}, map[string]any{"c": make(chan int)}); err == nil {
		t.Fatal("WriteYAML accepted an unmarshalable value")
	}
}

// controlCases are strings with control characters in them, each with the
// YAML scalar it is written as.
var controlCases = []struct{ in, yaml string }{
	{"a\x1b]52;c;Zm9v\x07b", `"a\x1b]52;c;Zm9v\ab"`},
	{"x\x1b[31mred", `"x\x1b[31mred"`},
	{"a\u009b31mb\u009d52;c;Zm9v\u009c", `"a\u009b31mb\u009d52;c;Zm9v\u009c"`},
	{"a\u0085b", `"a\u0085b"`},
	{"a\x7fb\x00c\bd", `"a\x7fb\x00c\bd"`},
	{"a\tb", `"a\tb"`},
	{"\tlead", `"\tlead"`},
	{"one\r\ntwo", `"one\r\ntwo"`},
	{"one\rtwo", `"one\rtwo"`},
	{"line\nnext \x1b[2J", `"line\nnext \x1b[2J"`},
	{"a\u2028b\u2029c", `"a\u2028b\u2029c"`},
	{"a\ufffeb", `"a\ufffeb"`},
	{"\n", `"\n"`},
	{"\n x", `"\n x"`},
	{"\n\n", `"\n\n"`},
}

// A string with a control character is double-quoted, the character
// escaped, so the output never carries one raw, and it parses back to the
// string it was.
func TestYAMLEscapesControlCharacters(t *testing.T) {
	for _, tc := range controlCases {
		var out bytes.Buffer
		if err := WriteYAML(&out, map[string]string{"v": tc.in}); err != nil {
			t.Fatalf("WriteYAML(%q) returned error: %v", tc.in, err)
		}
		if got, want := out.String(), "v: "+tc.yaml+"\n"; got != want {
			t.Errorf("WriteYAML(%q) = %q, want %q", tc.in, got, want)
		}
		var back map[string]string
		if err := yaml.Unmarshal(out.Bytes(), &back); err != nil || back["v"] != tc.in {
			t.Errorf("WriteYAML(%q) reads back as %q, %v", tc.in, back["v"], err)
		}
	}
}

// A key is a string like any other, and one that spans lines is quoted too:
// as a literal block it would not be a key.
func TestYAMLEscapesControlCharactersInKeys(t *testing.T) {
	keys := []string{"a\nb", "a\x1bb", "a\u009bb", "a\tb", "a\rb"}
	view := make(map[string]int, len(keys))
	for i, key := range keys {
		view[key] = i
	}
	var out bytes.Buffer
	if err := WriteYAML(&out, view); err != nil {
		t.Fatalf("WriteYAML returned error: %v", err)
	}
	want := `"a\tb": 3
"a\nb": 0
"a\rb": 4
"a\x1bb": 1
"a\u009bb": 2
`
	if got := out.String(); got != want {
		t.Fatalf("yaml mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
	var back map[string]int
	if err := yaml.Unmarshal(out.Bytes(), &back); err != nil || !reflect.DeepEqual(back, view) {
		t.Fatalf("keys read back as %v, %v", back, err)
	}
}

// Text that spans lines and has no other control character stays a literal
// block.
func TestYAMLKeepsLiteralBlocks(t *testing.T) {
	var out bytes.Buffer
	if err := WriteYAML(&out, map[string]string{"v": "line one\n\n  indented\nlast\n"}); err != nil {
		t.Fatalf("WriteYAML returned error: %v", err)
	}
	if got, want := out.String(), "v: |\n  line one\n  \n    indented\n  last\n"; got != want {
		t.Fatalf("WriteYAML = %q, want %q", got, want)
	}
}

// encoding/json escapes the C0 controls but writes DEL and the C1 controls
// raw; WriteJSON escapes them as well, and the strings decode as they were.
func TestWriteJSONEscapesDELAndC1(t *testing.T) {
	in := map[string]string{"a\u009bkey": "x\x7fy\u009b31m\u009d52;c;Zm9v\u009c\x1b\u00a0ü"}
	var out bytes.Buffer
	if err := WriteJSON(&out, in); err != nil {
		t.Fatalf("WriteJSON returned error: %v", err)
	}
	want := "{\n  \"a\\u009bkey\": \"x\\u007fy\\u009b31m\\u009d52;c;Zm9v\\u009c\\u001b\u00a0ü\"\n}\n"
	if got := out.String(); got != want {
		t.Fatalf("WriteJSON = %q, want %q", got, want)
	}
	var back map[string]string
	if err := json.Unmarshal(out.Bytes(), &back); err != nil || !reflect.DeepEqual(back, in) {
		t.Fatalf("WriteJSON output decodes to %q, %v", back, err)
	}
}

// rawControl returns the first control character in out other than the
// newline, and whether there is one.
func rawControl(out string) (rune, bool) {
	for _, r := range out {
		if r != '\n' && (r < 0x20 || (r >= 0x7f && r <= 0x9f)) {
			return r, true
		}
	}
	return 0, false
}

// Whatever a string holds, neither writer lets a control character through
// raw, and both read back as the JSON view of the value.
func FuzzStructuredControls(f *testing.F) {
	for _, tc := range controlCases {
		f.Add(tc.in)
	}
	f.Add("plain")
	f.Add("x\n  y")
	f.Add("- a\nb: c")
	for _, s := range ambiguousScalars {
		f.Add(s)
	}
	for _, s := range plainScalars {
		f.Add(s)
	}
	f.Add("0<<")
	f.Add("line one\n\nline three\n\n")
	f.Fuzz(func(t *testing.T, s string) {
		view := map[string]any{"value": s, "list": []string{s, "x"}}
		if s != "value" && s != "list" {
			view[s] = map[string]string{"nested": s}
		}
		encoded, err := json.Marshal(view)
		if err != nil {
			t.Fatalf("json.Marshal returned error: %v", err)
		}
		var want any
		if err := json.Unmarshal(encoded, &want); err != nil {
			t.Fatalf("json.Unmarshal returned error: %v", err)
		}

		var j bytes.Buffer
		if err := WriteJSON(&j, view); err != nil {
			t.Fatalf("WriteJSON returned error: %v", err)
		}
		if r, ok := rawControl(j.String()); ok {
			t.Fatalf("WriteJSON(%q) wrote %U raw", s, r)
		}
		var fromJSON any
		if err := json.Unmarshal(j.Bytes(), &fromJSON); err != nil || !reflect.DeepEqual(fromJSON, want) {
			t.Fatalf("WriteJSON(%q) decodes to %v, %v", s, fromJSON, err)
		}

		var y bytes.Buffer
		if err := WriteYAML(&y, view); err != nil {
			t.Fatalf("WriteYAML returned error: %v", err)
		}
		if !utf8.Valid(y.Bytes()) {
			t.Fatalf("WriteYAML(%q) wrote invalid UTF-8: %q", s, y.String())
		}
		if r, ok := rawControl(y.String()); ok {
			t.Fatalf("WriteYAML(%q) wrote %U raw:\n%s", s, r, y.String())
		}
		// goccy/go-yaml reads quickly but has quirks of its own, such as a key
		// "0<<" it does not parse; where it disagrees, PyYAML has the say.
		var fromYAML any
		err = yaml.Unmarshal(y.Bytes(), &fromYAML)
		if err == nil && reflect.DeepEqual(fromYAML, want) {
			return
		}
		if parse := pythonYAML(); parse != nil {
			fromYAML, err = parse(y.Bytes())
		}
		if err != nil {
			t.Fatalf("WriteYAML(%q) does not parse: %v\n%s", s, err, y.String())
		}
		if !reflect.DeepEqual(fromYAML, want) {
			t.Fatalf("WriteYAML(%q) reads back as %#v, want %#v\n%s", s, fromYAML, want, y.String())
		}
	})
}

// pythonYAML returns a function that parses YAML with PyYAML and returns the
// document as encoding/json decodes it, or nil when python3 or its yaml
// module is missing.
var pythonYAML = sync.OnceValue(func() func([]byte) (any, error) {
	python, err := exec.LookPath("python3")
	if err != nil || exec.Command(python, "-c", "import yaml").Run() != nil {
		return nil
	}
	const script = "import json,sys,yaml; json.dump(yaml.safe_load(sys.stdin), sys.stdout)"
	return func(document []byte) (any, error) {
		command := exec.Command(python, "-c", script)
		command.Stdin = bytes.NewReader(document)
		parsed, err := command.Output()
		if err != nil {
			return nil, err
		}
		var v any
		err = json.Unmarshal(parsed, &v)
		return v, err
	}
})

// The structured formats are the same view twice: whatever JSON writes, YAML
// has to parse back to. A real parser is the only honest check, so the test
// runs one when the machine has it.
func TestYAMLParsesBackToTheJSONView(t *testing.T) {
	parse := pythonYAML()
	if parse == nil {
		t.Skip("python3 or its yaml module is not available")
	}
	controls := make(map[string]string, len(controlCases))
	for _, tc := range controlCases {
		controls[tc.in] = tc.in
	}
	for name, view := range map[string]any{
		"edge":     sampleEdgeCase(),
		"nested":   nested{Name: "n", Items: []item{{"no", 0}, {"off", 3}}, Took: duration(time.Hour)},
		"controls": controls,
		"quirks":   []string{"...", "--- x", "? x", "<<", "=", ".inf", "-.Inf", ".NaN", "\ufeffx", "x\n  ", "\n x"},
		"keys":     map[string]string{"...": "a", "---": "b", "<<": "c", "\ufeffx": "d", "a\nb": "e", "=": "f"},
		"root":     "...",
		"scalars":  ambiguousScalars,
		"plain":    plainScalars,
		"numbers":  []any{1e-7, 1e21, 1e20, 2.5e-10, -0.5, uint64(math.MaxUint64), json.Number("123456789012345678901234567890")},
		"repeated": repeated{},
		"longkey":  map[string]any{strings.Repeat("k", 1500): []int{1}, "0<<": map[string]string{strings.Repeat("k", 1100) + ": x": "v"}},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := WriteYAML(&out, view); err != nil {
				t.Fatalf("WriteYAML returned error: %v", err)
			}
			fromYAML, err := parse(out.Bytes())
			if err != nil {
				t.Fatalf("parsing the YAML failed: %v\n%s", err, out.String())
			}
			encoded, err := json.Marshal(view)
			if err != nil {
				t.Fatalf("json.Marshal returned error: %v", err)
			}
			var fromJSON any
			if err := json.Unmarshal(encoded, &fromJSON); err != nil {
				t.Fatalf("re-reading the JSON failed: %v", err)
			}
			if !reflect.DeepEqual(fromYAML, fromJSON) {
				t.Fatalf("YAML does not carry the JSON view\nyaml: %v\njson: %s\n%s", fromYAML, encoded, out.String())
			}
		})
	}
}
