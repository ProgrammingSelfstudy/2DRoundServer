// Package logfile is a log writer that writes to a file and rolls it over when it gets big.
//
// The current log is <dir>/<name>.log; a roll renames it to <name>.log.1, the older ones to
// .2, .3, ... and removes what is beyond the number to keep. Files are private (0600, in a
// 0700 directory): logs name accounts and addresses.
package logfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Writer is safe for concurrent use.
type Writer struct {
	dir, name string
	max       int64
	keep      int

	mu   sync.Mutex
	f    *os.File
	size int64
}

// Open opens (appending to it if it exists) <dir>/<name>.log. maxBytes is the size at which
// the file is rolled over (0 = never), keep how many rolled files stay (at least 1).
func Open(dir, name string, maxBytes int64, keep int) (*Writer, error) {
	if keep < 1 {
		keep = 1
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	w := &Writer{dir: dir, name: name, max: maxBytes, keep: keep}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) path(n int) string {
	p := filepath.Join(w.dir, w.name+".log")
	if n > 0 {
		p += fmt.Sprintf(".%d", n)
	}
	return p
}

func (w *Writer) open() error {
	f, err := os.OpenFile(w.path(0), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.f, w.size = f, info.Size()
	return nil
}

// roll moves the current file to .1 (and .1 to .2, ...) and starts a new one.
func (w *Writer) roll() error {
	if err := w.f.Close(); err != nil {
		return err
	}
	// rename replaces its target on Unix; Windows refuses, so the oldest file goes first
	os.Remove(w.path(w.keep))
	for n := w.keep - 1; n >= 1; n-- {
		if err := os.Rename(w.path(n), w.path(n+1)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(w.path(0), w.path(1)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return w.open()
}

// Write appends p. A line is never split between two files: the roll happens before the
// write that would pass the limit.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.max > 0 && w.size > 0 && w.size+int64(len(p)) > w.max {
		if err := w.roll(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}
