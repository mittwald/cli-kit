package term

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestLinkify(t *testing.T) {
	got := Linkify("see https://example.com/a?b=1. or (http://x.test)")
	want := "see " + Hyperlink("https://example.com/a?b=1", "https://example.com/a?b=1") + ". or (" +
		Hyperlink("http://x.test", "http://x.test") + ")"
	if got != want {
		t.Errorf("Linkify = %q\nwant      %q", got, want)
	}
	if got := Linkify("no links here"); got != "no links here" {
		t.Errorf("Linkify changed plain text: %q", got)
	}
	if got := Hyperlink("", "text"); got != "text" {
		t.Errorf("Hyperlink without URL = %q", got)
	}
}

func TestLinksPassesThroughNonTerminal(t *testing.T) {
	var buf bytes.Buffer
	if w := Links(&buf); w != &buf {
		t.Error("Links wrapped a non-terminal writer")
	}
	lw := linkWriter{w: &buf}
	if _, err := lw.Write([]byte("go to https://example.com\n")); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("\x1b]8;;https://example.com\x1b\\")) {
		t.Errorf("linkWriter did not hyperlink: %q", buf.String())
	}
}

// A URL with a control character in it would end the escape sequence it is
// written into and start one of its own, so it is no link.
func TestHyperlinkRefusesControlCharacters(t *testing.T) {
	for name, url := range map[string]string{
		"escape":        "https://x.test/\x1b\\\x1b]52;c;ZXZpbA==\x07",
		"bel":           "https://x.test/\x07rest",
		"newline":       "https://x.test/\nrest",
		"del":           "https://x.test/\x7f",
		"8-bit st":      "https://x.test/\u009c\u009d52;c;ZXZpbA==\u009c",
		"invalid utf-8": "https://x.test/\x9c",
	} {
		if got := Hyperlink(url, "click"); got != "click" {
			t.Errorf("%s: Hyperlink(%q) = %q, want the text alone", name, url, got)
		}
	}
	if got, want := Hyperlink("https://x.test/ä?q=日本", "click"), "\x1b]8;;https://x.test/ä?q=日本\x1b\\click\x1b]8;;\x1b\\"; got != want {
		t.Errorf("Hyperlink of a URL with letters beyond ASCII = %q, want %q", got, want)
	}
}

// Linkify reads text only: what an escape sequence holds, and the text of a
// hyperlink that is already there, stay as they are.
func TestLinkifyLeavesEscapeSequencesAlone(t *testing.T) {
	const url = "https://example.com/docs"
	link := Hyperlink(url, url)
	for name, in := range map[string]string{
		"hyperlink with a label":     "see " + Hyperlink(url, "the docs") + " now",
		"hyperlink labeled a url":    link,
		"bel-terminated hyperlink":   "\x1b]8;;" + url + "\x07" + url + "\x1b]8;;\x07",
		"hyperlink with parameters":  "\x1b]8;id=1;" + url + "\x1b\\" + url + "\x1b]8;;\x1b\\",
		"styled label":               "\x1b]8;;" + url + "\x1b\\\x1b[4m" + url + "\x1b[0m\x1b]8;;\x1b\\",
		"window title":               "\x1b]0;" + url + "\x07text",
		"hyperlink left open":        "\x1b]8;;" + url + "\x1b\\" + url + " and " + url,
		"linkified before":           Linkify("see " + url + ", then (http://x.test)."),
		"no url":                     "ftp://example.com and ://",
		"control inside the url":     "https://\x07example.com",
		"escape right after scheme":  "https://\x1b[1mexample.com",
		"invalid utf-8 in the url":   "https://example.com/\xff",
		"8-bit sequence in the url":  "https://\u009d52;c;Zm9v\u009c",
		"only punctuation after it":  "see https://). and http://...",
		"hyperlink, then plain text": link + " plain",
	} {
		if got := Linkify(in); got != in {
			t.Errorf("%s: Linkify(%q) =\n%q, want it unchanged", name, in, got)
		}
	}
	// Text around a hyperlink is still linkified, and a style sequence ends
	// a URL without becoming part of it.
	if got, want := Linkify(Hyperlink(url, "docs")+" or "+url), Hyperlink(url, "docs")+" or "+link; got != want {
		t.Errorf("Linkify after a hyperlink = %q, want %q", got, want)
	}
	if got, want := Linkify("\x1b[4m"+url+"\x1b[0m"), "\x1b[4m"+link+"\x1b[0m"; got != want {
		t.Errorf("Linkify of a styled URL = %q, want %q", got, want)
	}
	// A control character ends a URL.
	if got, want := Linkify(url+"\x07x"), link+"\x07x"; got != want {
		t.Errorf("Linkify of a URL before a control character = %q, want %q", got, want)
	}
}

// Punctuation after a URL belongs to the sentence, a closing bracket only
// when the URL does not open it.
func TestLinkifyTrimsPunctuation(t *testing.T) {
	tests := map[string]struct{ in, link, rest string }{
		"period":              {"https://example.com/a.", "https://example.com/a", "."},
		"several":             {"https://example.com/a?!;:,.", "https://example.com/a", "?!;:,."},
		"in parentheses":      {"http://x.test)", "http://x.test", ")"},
		"wikipedia":           {"https://en.wikipedia.org/wiki/Go_(programming_language)", "https://en.wikipedia.org/wiki/Go_(programming_language)", ""},
		"wikipedia, sentence": {"https://en.wikipedia.org/wiki/Go_(game)).", "https://en.wikipedia.org/wiki/Go_(game)", ")."},
		"ipv6":                {"http://[::1]", "http://[::1]", ""},
		"ipv6 with a port":    {"http://[::1]:8080/x]", "http://[::1]:8080/x", "]"},
		"ipv6 in brackets":    {"http://[::1]].", "http://[::1]", "]."},
		"period in brackets":  {"https://x.test/(a.)", "https://x.test/(a.)", ""},
		"mixed brackets":      {"https://x.test/{a}(b)]})", "https://x.test/{a}(b)", "]})"},
		"closed twice":        {"https://x.test/(a))", "https://x.test/(a)", ")"},
		"opened later":        {"https://x.test/a)(b)", "https://x.test/a)(b)", ""},
		"query kept":          {"https://example.com/a?b=1", "https://example.com/a?b=1", ""},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got, want := Linkify(tt.in), Hyperlink(tt.link, tt.link)+tt.rest; got != want {
				t.Errorf("Linkify(%q) =\n%q\nwant\n%q", tt.in, got, want)
			}
		})
	}
}

// A URL followed by a long run of closing brackets is trimmed in time linear
// in its length.
func TestLinkifyManyBracketsIsLinear(t *testing.T) {
	in := "https://x.test/" + strings.Repeat("(", 100000) + strings.Repeat(")", 200000)
	start := time.Now()
	got := Linkify(in)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Linkify of %d brackets took %v", 300000, elapsed)
	}
	link := in[:len(in)-100000]
	if want := Hyperlink(link, link) + strings.Repeat(")", 100000); got != want {
		t.Fatal("the brackets the URL opens are not the ones it keeps")
	}
}

// What is written through Links is linkified as Linkify does it: a
// hyperlink that is already there passes through as it is.
func TestLinkWriterKeepsHyperlinks(t *testing.T) {
	var buf bytes.Buffer
	lw := linkWriter{w: &buf}
	in := "create a token at " + Hyperlink("https://example.com/tokens", "the token page") + "\n"
	n, err := lw.Write([]byte(in))
	if err != nil || n != len(in) {
		t.Fatalf("Write = %d, %v, want %d, nil", n, err, len(in))
	}
	if buf.String() != in {
		t.Errorf("linkWriter wrote %q, want %q", buf.String(), in)
	}
}
