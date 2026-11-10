package term

import (
	"bytes"
	"io"
	"regexp"
	"strings"
)

// Hyperlink wraps text in an OSC 8 hyperlink to url. Terminals that support
// OSC 8 make text clickable; all others print text alone.
func Hyperlink(url, text string) string {
	if url == "" {
		return text
	}
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// urlPattern is what counts as a link in running text. Trailing punctuation
// ends a sentence more often than a URL, so [Linkify] trims it off.
var urlPattern = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `\x1b]+`)

// Linkify wraps every http and https URL in s in an OSC 8 hyperlink.
func Linkify(s string) string {
	return urlPattern.ReplaceAllStringFunc(s, func(match string) string {
		link := strings.TrimRight(match, ".,;:!?)]}")
		return Hyperlink(link, link) + match[len(link):]
	})
}

// linkWriter hyperlinks the URLs written through it.
type linkWriter struct{ w io.Writer }

func (l linkWriter) Unwrap() io.Writer { return l.w }

func (l linkWriter) Write(p []byte) (int, error) {
	if !bytes.Contains(p, []byte("://")) {
		return l.w.Write(p)
	}
	if _, err := io.WriteString(l.w, Linkify(string(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Links returns a writer that hyperlinks the URLs written through it when w
// is a terminal that may be painted, and w itself otherwise. A URL split
// across two writes is not recognized, so write whole lines.
func Links(w io.Writer) io.Writer {
	if !CanAnimate(w) {
		return w
	}
	return linkWriter{w: w}
}
