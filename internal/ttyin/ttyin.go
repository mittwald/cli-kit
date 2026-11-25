// Package ttyin reads a terminal in raw mode in a way another goroutine can
// interrupt: a read waits on the terminal and on a pipe at once, so a
// program that is done with the terminal can end a read that is still
// blocked, rather than leave a goroutine behind that takes the next key
// typed into the shell.
//
// Where the descriptors cannot be waited on (Windows, or a descriptor past
// what select(2) takes on macOS) a read blocks on the terminal alone and
// cannot be woken; [Reader.Wakes] reports which of the two applies.
package ttyin

import (
	"errors"
	"os"
	"sync"
)

// ErrWoken is what [Reader.Read] returns when [Reader.Wake] ended it rather
// than input.
var ErrWoken = errors.New("ttyin: read woken")

// Reader reads from a terminal and can be woken.
type Reader struct {
	file *os.File
	// wakeRead and wakeWrite are the two ends of the pipe a read waits on
	// next to the terminal, nil where a read cannot be woken.
	wakeRead, wakeWrite *os.File
	wakeFd              int
	closeOnce           sync.Once
}

// NewReader returns a reader of file, a terminal already in raw mode.
func NewReader(file *os.File) *Reader {
	r := &Reader{file: file, wakeFd: -1}
	if !awaitable(file.Fd()) {
		return r
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return r
	}
	if !awaitable(pr.Fd()) {
		_ = pr.Close()
		_ = pw.Close()
		return r
	}
	r.wakeRead, r.wakeWrite, r.wakeFd = pr, pw, int(pr.Fd())
	return r
}

// awaitable reports whether awaitInput can wait on the descriptor fd.
func awaitable(fd uintptr) bool {
	return maxAwaitFd >= 0 && fd <= uintptr(maxAwaitFd)
}

// Read blocks until the terminal has input and reads it into p, or until
// Wake was called, when it returns ErrWoken and reads nothing.
func (r *Reader) Read(p []byte) (int, error) {
	if r.Wakes() {
		if _, woken := awaitInput(r.file, r.wakeFd, -1); woken {
			var drain [64]byte
			_, _ = r.wakeRead.Read(drain[:])
			return 0, ErrWoken
		}
	}
	return r.file.Read(p)
}

// Wakes reports whether Wake can end a read that is already blocked.
func (r *Reader) Wakes() bool { return r.wakeRead != nil }

// Wake makes a blocked read, or else the next one, return ErrWoken. It may
// be called from any goroutine, also after Close.
func (r *Reader) Wake() {
	if r.wakeWrite != nil {
		_, _ = r.wakeWrite.Write([]byte{0})
	}
}

// Close lets go of the pipe. Call it once no read is in progress any more.
func (r *Reader) Close() {
	r.closeOnce.Do(func() {
		if r.wakeRead != nil {
			_ = r.wakeRead.Close()
			_ = r.wakeWrite.Close()
		}
	})
}
