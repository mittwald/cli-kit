package term

import (
	"bytes"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Hyperlink wraps text in an OSC 8 hyperlink to url. Terminals that support
// OSC 8 make text clickable; all others print text alone.
//
// The url is written into an escape sequence, where a control character
// would end the sequence early and let the rest of the url act as one of its
// own: a clipboard write, say. A url that holds a control character (C0, DEL
// or C1) or is not valid UTF-8 is therefore no link, and Hyperlink returns
// text alone, as it does for an empty url. The text is written as it is:
// text from elsewhere goes through [Plain] or [PlainLine] first.
func Hyperlink(url, text string) string {
	if url == "" || !utf8.ValidString(url) || strings.ContainsFunc(url, control) {
		return text
	}
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// urlPattern is what counts as a link in running text: it ends at white
// space, at a quote or an angle bracket, and at a control character.
// Trailing punctuation ends a sentence more often than a URL, so [Linkify]
// trims it off.
var urlPattern = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `\x00-\x1f\x7f-\x{9f}]+`)

// Linkify wraps every http and https URL in s in an OSC 8 hyperlink. Only
// text is searched: a URL inside an escape sequence, such as the target of a
// hyperlink s already has or a window title, is left alone, and so is the
// text of a hyperlink that is open, so text that was linkified before comes
// back unchanged.
//
// Punctuation that ends a URL is taken for the sentence's and stays outside
// the link: a period, comma, semicolon, colon, exclamation or question mark,
// and a closing bracket that nothing in the URL opens. A bracket the URL
// opens itself belongs to it, as in https://en.wikipedia.org/wiki/Go_(game)
// or http://[::1].
func Linkify(s string) string {
	if !strings.Contains(s, "://") {
		return s
	}
	var (
		b    strings.Builder
		open openSequences
	)
	for i := 0; i < len(s); {
		if n := EscapeLen(s[i:]); n > 0 {
			seq := s[i : i+n]
			b.WriteString(seq)
			open.track(seq)
			i += n
			continue
		}
		next := len(s)
		if j := strings.IndexByte(s[i:], 0x1b); j >= 0 {
			next = i + j
		}
		text := s[i:next]
		if !open.linked {
			text = urlPattern.ReplaceAllStringFunc(text, func(match string) string {
				link := trimURL(match)
				if strings.HasSuffix(link, "://") {
					// A scheme with nothing but punctuation after it.
					return match
				}
				return Hyperlink(link, link) + match[len(link):]
			})
		}
		b.WriteString(text)
		i = next
	}
	return b.String()
}

// trimURL returns match, a URL found in running text, without the
// punctuation at its end that belongs to the sentence around it: . , ; : !
// and ?, and every closing bracket that nothing in match opens.
func trimURL(match string) string {
	const closing, punctuation = ")]}", ".,;:!?)]}"
	// Only the punctuation match ends with can go. Brackets before it are
	// counted, so that a closing one among it finds its opening one.
	start := len(strings.TrimRight(match, punctuation))
	var depth [len(closing)]int
	for i := range start {
		if kind := strings.IndexByte("([{", match[i]); kind >= 0 {
			depth[kind]++
		} else if kind := strings.IndexByte(closing, match[i]); kind >= 0 && depth[kind] > 0 {
			depth[kind]--
		}
	}
	// The URL ends with the last bracket that closes one of its own.
	end := start
	for i := start; i < len(match); i++ {
		if kind := strings.IndexByte(closing, match[i]); kind >= 0 && depth[kind] > 0 {
			depth[kind]--
			end = i + 1
		}
	}
	return match[:end]
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

// Links returns a writer that hyperlinks the URLs written through it, as
// [Linkify] does, when w is a terminal that may be painted, and w itself
// otherwise. A URL split across two writes is not recognized, and neither is
// a hyperlink that one write opens and another closes, so write whole lines.
func Links(w io.Writer) io.Writer {
	if !CanAnimate(w) {
		return w
	}
	return linkWriter{w: w}
}
