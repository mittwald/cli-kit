package format

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
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

// The library quotes every scalar whose plain form would come back as another
// type — a time, a date, a boolean, a number — and leaves the rest readable.
// The one case it does not quote is a digit string with a leading zero
// (0815): goccy/go-yaml and PyYAML both read it back as a string, since it is
// not valid octal, but a YAML 1.2 core-schema reader would take it for 815.
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
numlike: 0815
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

// The structured formats are the same view twice: whatever JSON writes, YAML
// has to parse back to. A real parser is the only honest check, so the test
// runs one when the machine has it.
func TestYAMLParsesBackToTheJSONView(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not available")
	}
	if err := exec.Command(python, "-c", "import yaml").Run(); err != nil {
		t.Skip("python3 has no yaml module")
	}
	const script = "import json,sys,yaml; json.dump(yaml.safe_load(sys.stdin), sys.stdout)"
	for name, view := range map[string]any{
		"edge":   sampleEdgeCase(),
		"nested": nested{Name: "n", Items: []item{{"no", 0}, {"off", 3}}, Took: duration(time.Hour)},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := WriteYAML(&out, view); err != nil {
				t.Fatalf("WriteYAML returned error: %v", err)
			}
			command := exec.Command(python, "-c", script)
			command.Stdin = strings.NewReader(out.String())
			parsed, err := command.Output()
			if err != nil {
				t.Fatalf("parsing the YAML failed: %v\n%s", err, out.String())
			}
			encoded, err := json.Marshal(view)
			if err != nil {
				t.Fatalf("json.Marshal returned error: %v", err)
			}
			var fromYAML, fromJSON any
			if err := json.Unmarshal(parsed, &fromYAML); err != nil {
				t.Fatalf("re-reading the parsed YAML failed: %v", err)
			}
			if err := json.Unmarshal(encoded, &fromJSON); err != nil {
				t.Fatalf("re-reading the JSON failed: %v", err)
			}
			if !reflect.DeepEqual(fromYAML, fromJSON) {
				t.Fatalf("YAML does not carry the JSON view\nyaml: %s\njson: %s", parsed, encoded)
			}
		})
	}
}
