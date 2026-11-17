package secret

import (
	"bytes"
	"errors"
	"io"
	"strings"
)

// readFirstLine reads the first line of rd. It stops at the newline, so a
// pipe that stays open after the secret does not block, and the rest of
// stdin stays unread. A trailing CR is dropped, so a file written on
// Windows works, and so is a UTF-8 byte order mark in front; interior
// spaces are kept, because a passphrase may contain them.
//
// A line longer than MaxLen is refused with ErrTooLong rather than cut
// short, and one that is no text (UTF-16, or holding a NUL byte) with an
// error wrapping ErrNotText. Neither error carries what was read.
func (r *Resolver) readFirstLine(rd io.Reader) (string, error) {
	limit := r.maxLen()
	data := make([]byte, 0, min(limit, 256))
	var b [1]byte
	for {
		n, err := rd.Read(b[:])
		if n == 1 {
			if b[0] == '\n' {
				break
			}
			data = append(data, b[0])
			// The first bytes of a byte order mark exceed a short limit
			// before they are one: they are judged once they are complete,
			// or turned out to be something else.
			if !startsByteOrderMark(data) && tooLong(data, limit) {
				if textErr := checkText(data); textErr != nil {
					return "", textErr
				}
				return "", ErrTooLong
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
	}
	if err := checkText(data); err != nil {
		return "", err
	}
	// A line that ended inside what could have become a byte order mark was
	// not judged above.
	if tooLong(data, limit) {
		return "", ErrTooLong
	}
	line := strings.TrimSuffix(strings.TrimPrefix(string(data), utf8BOM), "\r")
	if r.TrimSpace {
		line = strings.TrimSpace(line)
	}
	return line, nil
}

// tooLong reports whether data, the start of a line, already holds more
// than limit bytes of secret. A byte order mark in front does not count,
// and neither does a CR at the end, which may turn out to belong to the
// line ending.
func tooLong(data []byte, limit int) bool {
	n := len(data)
	if bytes.HasPrefix(data, []byte(utf8BOM)) {
		n -= len(utf8BOM)
	}
	if n > 0 && data[len(data)-1] == '\r' {
		n--
	}
	return n > limit
}

// startsByteOrderMark reports whether data is the beginning of a UTF-8 byte
// order mark that further bytes have yet to complete.
func startsByteOrderMark(data []byte) bool {
	return len(data) < len(utf8BOM) && strings.HasPrefix(utf8BOM, string(data))
}

// checkText refuses a line that is not text in an encoding a secret can be
// read from: UTF-16, which starts with a byte order mark, and anything with
// a NUL byte, which UTF-16 without the mark is full of.
func checkText(data []byte) error {
	switch {
	case bytes.HasPrefix(data, []byte("\xff\xfe")), bytes.HasPrefix(data, []byte("\xfe\xff")):
		return errUTF16
	case bytes.IndexByte(data, 0) >= 0:
		return errNUL
	}
	return nil
}
