package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Rotating is an io.Writer that appends to dir/name.txt and rotates it to
// name.1.txt … name.<keep-1>.txt when it grows past maxBytes.
type Rotating struct {
	dir, name string
	maxBytes  int64
	keep      int

	mu   sync.Mutex
	f    *os.File
	size int64
}

// OpenRotating opens (or creates) the current log file.
func OpenRotating(dir, name string, maxBytes int64, keep int) (*Rotating, error) {
	if err := os.MkdirAll(dir, 0o775); err != nil {
		return nil, err
	}
	r := &Rotating{dir: dir, name: name, maxBytes: maxBytes, keep: max(keep, 1)}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Rotating) path(i int) string {
	if i == 0 {
		return filepath.Join(r.dir, r.name+".txt")
	}
	return filepath.Join(r.dir, r.name+"."+strconv.Itoa(i)+".txt")
}

func (r *Rotating) open() error {
	f, err := os.OpenFile(r.path(0), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o664)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, st.Size()
	return nil
}

func (r *Rotating) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return 0, os.ErrClosed
	}
	if r.size > 0 && r.size+int64(len(p)) > r.maxBytes {
		if err := r.rotate(); err != nil {
			// keep logging to the current file rather than losing lines
			fmt.Fprintln(os.Stderr, "log rotation failed:", err)
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *Rotating) rotate() error {
	if err := r.f.Close(); err != nil {
		return err
	}
	r.f = nil
	_ = os.Remove(r.path(r.keep - 1))
	for i := r.keep - 2; i >= 0; i-- {
		if _, err := os.Stat(r.path(i)); err == nil {
			if err := os.Rename(r.path(i), r.path(i+1)); err != nil {
				return err
			}
		}
	}
	return r.open()
}

// Close closes the current file.
func (r *Rotating) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}

// LogFile describes a log file on disk.
type LogFile struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

// Files lists the log files in dir, newest first.
func Files(dir string) []LogFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []LogFile{}
	}
	out := []LogFile{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		if info, err := e.Info(); err == nil {
			out = append(out, LogFile{Name: e.Name(), Size: info.Size(), Modified: info.ModTime()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out
}
