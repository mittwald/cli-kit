package term

import (
	"bytes"
	"testing"
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
