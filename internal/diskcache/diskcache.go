// Package diskcache inspects and trims the on-disk image cache
// (<data>/cache/<bucket>/...: thumbnails, extension icons, covers).
package diskcache

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Buckets are the cache folders.
var Buckets = []string{"thumbs", "assets", "covers", "pages"}

// BucketStats describes one cache folder.
type BucketStats struct {
	Name  string `json:"name"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

type file struct {
	path string
	size int64
	mod  time.Time
}

func walk(dir string, fn func(file)) {
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		fn(file{p, info.Size(), info.ModTime()})
		return nil
	})
}

// Stats reports each bucket under root.
func Stats(root string) []BucketStats {
	out := make([]BucketStats, 0, len(Buckets))
	for _, b := range Buckets {
		st := BucketStats{Name: b}
		walk(filepath.Join(root, b), func(f file) {
			if !strings.HasSuffix(f.path, ".type") {
				st.Files++
			}
			st.Bytes += f.size
		})
		out = append(out, st)
	}
	return out
}

// Clear removes the given buckets (all when empty).
func Clear(root string, buckets []string) error {
	if len(buckets) == 0 {
		buckets = Buckets
	}
	for _, b := range buckets {
		if err := os.RemoveAll(filepath.Join(root, filepath.Base(b))); err != nil {
			return err
		}
	}
	return nil
}

// Trim deletes files older than maxAge and then the oldest files until the
// cache is below maxBytes (0 = no size cap). It returns the files removed.
func Trim(root string, maxAge time.Duration, maxBytes int64) int {
	var files []file
	var total int64
	walk(root, func(f file) {
		if filepath.Base(f.path) == ".format" {
			return
		}
		files = append(files, f)
		total += f.size
	})
	removed := 0
	cutoff := time.Now().Add(-maxAge)
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	for _, f := range files {
		if !(maxAge > 0 && f.mod.Before(cutoff)) && !(maxBytes > 0 && total > maxBytes) {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
			if !strings.HasSuffix(f.path, ".type") {
				removed++
			}
		}
	}
	return removed
}
