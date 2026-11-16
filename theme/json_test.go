package theme

import (
	"encoding/json"
	"strings"
	"testing"
)

// jsonTheme paints each JSON role in a style that names it.
var jsonTheme = Theme{JSONKey: "k", JSONString: "s", JSONNumber: "n", JSONBool: "b", JSONNull: "z"}

func TestPaintJSON(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"compact object", `{"a":1,"b":"x"}`,
			"{\x1b[km\"a\"\x1b[0m:\x1b[nm1\x1b[0m,\x1b[km\"b\"\x1b[0m:\x1b[sm\"x\"\x1b[0m}"},
		{"indented with space before colon", "{\n  \"a\" : true\n}",
			"{\n  \x1b[km\"a\"\x1b[0m : \x1b[bmtrue\x1b[0m\n}"},
		{"literals", `[true,false,null]`,
			"[\x1b[bmtrue\x1b[0m,\x1b[bmfalse\x1b[0m,\x1b[zmnull\x1b[0m]"},
		{"numbers", `[-1.5e+3,0,10E-2]`,
			"[\x1b[nm-1.5e+3\x1b[0m,\x1b[nm0\x1b[0m,\x1b[nm10E-2\x1b[0m]"},
		{"escaped quote and colon inside a string", `["a\":b"]`,
			"[\x1b[sm\"a\\\":b\"\x1b[0m]"},
		{"string value ending in a backslash", `{"p":"C:\\"}`,
			"{\x1b[km\"p\"\x1b[0m:\x1b[sm\"C:\\\\\"\x1b[0m}"},
		{"words that are not JSON stay plain", `[True, nan, 01]`,
			"[True, nan, 01]"},
		{"unclosed string ends at the line", "[\"open\n1]",
			"[\x1b[sm\"open\x1b[0m\n\x1b[nm1\x1b[0m]"},
		{"unicode kept", `{"name":"grün ✓"}`,
			"{\x1b[km\"name\"\x1b[0m:\x1b[sm\"grün ✓\"\x1b[0m}"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jsonTheme.PaintJSON(tt.in); got != tt.want {
				t.Errorf("PaintJSON(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

// A theme without JSON roles, Plain among them, leaves the text alone.
func TestPaintJSONWithoutRoles(t *testing.T) {
	in := `{"a": [1, true, null, "x"]}`
	for _, th := range []Theme{Plain, {Added: "32"}} {
		if got := th.PaintJSON(in); got != in {
			t.Errorf("PaintJSON = %q, want it unchanged", got)
		}
	}
}

// Painting only adds escape sequences: what is left without them is the
// JSON as it was, and every painted span is closed before the line ends.
func TestPaintJSONKeepsText(t *testing.T) {
	data, err := json.MarshalIndent(map[string]any{
		"name": "a \"quoted\" \x1b[31m value", "count": 12, "ratio": -0.5,
		"ok": true, "none": nil, "list": []any{"x", 1e21, false},
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	painted := Default.PaintJSON(string(data))
	if got := stripSGR(painted); got != string(data) {
		t.Fatalf("text changed:\n got %q\nwant %q", got, data)
	}
	for line := range strings.Lines(painted) {
		if strings.Count(line, "\x1b[38;5;") != strings.Count(line, Reset) {
			t.Errorf("line %q leaves a color open", line)
		}
	}
}

// stripSGR removes the SGR sequences Paint writes.
func stripSGR(s string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return b.String() + s
		}
		b.WriteString(s[:i])
		s = s[i+strings.IndexByte(s[i:], 'm')+1:]
	}
}
