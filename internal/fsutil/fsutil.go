// Package fsutil has small filesystem helpers.
package fsutil

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

// FreeSpace returns available bytes on the filesystem holding path.
func FreeSpace(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}

// Writable checks that dir exists and a file can be created in it.
func Writable(dir string) error {
	st, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return errors.New("not a directory")
	}
	f, err := os.CreateTemp(dir, ".mangarr-write-test-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}

// ParseMode parses an octal mode string ("0664"), falling back to def.
func ParseMode(s string, def os.FileMode) os.FileMode {
	if s == "" {
		return def
	}
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return def
	}
	return os.FileMode(v)
}

// LinkOrCopy hardlinks src to dst, falling back to a copy (e.g. across devices).
func LinkOrCopy(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o775); err != nil {
		return err
	}
	_ = os.Remove(dst)
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	return CopyFile(src, dst)
}

// CopyFile copies src to dst (fsynced).
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, st.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// Move renames src to dst, copying across devices when needed.
func Move(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o775); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := CopyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}
