// Package cbz writes and reads CBZ archives. Writes are atomic: the archive
// is written to "<name>.cbz.partial" in the destination folder (an extension
// neither Komga nor Kavita scans), fsynced, then renamed over the final path.
package cbz

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const PartialSuffix = ".partial"

type Page struct {
	// Name inside the archive, e.g. "0001.jpg".
	Name string
	// Path of the page on disk (used when Data is nil).
	Path string
	Data []byte
}

type Result struct {
	Size   int64
	SHA256 string
}

// Write creates dst atomically with ComicInfo.xml (if non-nil) and pages.
func Write(dst string, pages []Page, comicInfo []byte, mode fs.FileMode, modTime time.Time) (Result, error) {
	if mode == 0 {
		mode = 0o664
	}
	if modTime.IsZero() {
		modTime = time.Now()
	}
	tmp := dst + PartialSuffix
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return Result{}, err
	}
	cleanup := func() { _ = f.Close(); _ = os.Remove(tmp) }

	h := sha256.New()
	w := zip.NewWriter(io.MultiWriter(f, h))
	add := func(name string, r io.Reader) error {
		fw, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: modTime})
		if err != nil {
			return err
		}
		_, err = io.Copy(fw, r)
		return err
	}
	if comicInfo != nil {
		if err := add("ComicInfo.xml", strings.NewReader(string(comicInfo))); err != nil {
			cleanup()
			return Result{}, err
		}
	}
	sorted := append([]Page(nil), pages...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, p := range sorted {
		var err error
		if p.Data != nil {
			err = add(p.Name, strings.NewReader(string(p.Data)))
		} else {
			var pf *os.File
			pf, err = os.Open(p.Path)
			if err == nil {
				err = add(p.Name, pf)
				_ = pf.Close()
			}
		}
		if err != nil {
			cleanup()
			return Result{}, fmt.Errorf("add %s: %w", p.Name, err)
		}
	}
	if err := w.Close(); err != nil {
		cleanup()
		return Result{}, err
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return Result{}, err
	}
	st, err := f.Stat()
	if err != nil {
		cleanup()
		return Result{}, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return Result{}, err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return Result{}, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return Result{}, err
	}
	syncDir(filepath.Dir(dst))
	return Result{Size: st.Size(), SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// Read loads all pages (sorted) and ComicInfo.xml from a CBZ.
func Read(path string) (pages []Page, comicInfo []byte, err error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, nil, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, nil, err
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, nil, err
		}
		if strings.EqualFold(filepath.Base(f.Name), "ComicInfo.xml") {
			comicInfo = data
			continue
		}
		if isImageName(f.Name) {
			pages = append(pages, Page{Name: filepath.Base(f.Name), Data: data})
		}
	}
	sort.SliceStable(pages, func(i, j int) bool { return pages[i].Name < pages[j].Name })
	return pages, comicInfo, nil
}

func isImageName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".jxl", ".bmp":
		return true
	}
	return false
}

// PageName returns the archive name for page index i (0-based) and extension.
func PageName(i int, ext string) string { return fmt.Sprintf("%04d%s", i+1, ext) }

// Entry is a page image in a CBZ.
type Entry struct {
	// Name is the page's base name; Path its path inside the archive.
	Name string
	Path string
	Size int64
}

// List lists the page images of a CBZ (sorted like Read) without reading them.
func List(path string) ([]Entry, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var out []Entry
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() && isImageName(f.Name) {
			out = append(out, Entry{Name: filepath.Base(f.Name), Path: f.Name, Size: int64(f.UncompressedSize64)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ReadEntry reads one file of a CBZ by its path inside the archive.
func ReadEntry(path, name string) ([]byte, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, 128<<20))
	}
	return nil, fs.ErrNotExist
}
