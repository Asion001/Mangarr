// Package backupimport reads library backups of other manga apps (Mihon,
// Tachiyomi and its forks, Suwayomi, Aidoku) into one normalized model.
// Mapping the entries to catalogs and adding them lives in internal/imports.
package backupimport

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Formats.
const (
	FormatMihon  = "mihon"  // Mihon, Tachiyomi, forks and Suwayomi (.tachibk, .proto.gz)
	FormatAidoku = "aidoku" // Aidoku (.aib)
)

// Tracker names used in Entry.Trackers (match metadata provider names).
const (
	TrackerAniList      = "anilist"
	TrackerMAL          = "mal"
	TrackerMangaUpdates = "mangaupdates"
	TrackerKitsu        = "kitsu"
)

// Backup is a parsed backup.
type Backup struct {
	Format string `json:"format"`
	// CreatedAt is when the backup was made (zero when unknown).
	CreatedAt time.Time `json:"createdAt,omitzero"`
	// Sources maps source ids to their names.
	Sources    map[string]string `json:"sources"`
	Categories []string          `json:"categories"`
	Entries    []Entry           `json:"entries"`
}

// Entry is one manga of the backup.
type Entry struct {
	// SourceID is the app's source id: a Mihon source id (decimal int64,
	// the same as Keiyoushi/Suwayomi catalog ids) or an Aidoku source id
	// such as "multi.mangadex".
	SourceID   string `json:"sourceId"`
	SourceName string `json:"sourceName,omitempty"`
	// URL is the source's manga key: a path for Mihon ("/manga/123"), the
	// manga id for Aidoku.
	URL string `json:"url"`
	// WebURL is the full web address when the app stores it (Aidoku).
	WebURL       string    `json:"webUrl,omitempty"`
	Title        string    `json:"title"`
	Author       string    `json:"author,omitempty"`
	Artist       string    `json:"artist,omitempty"`
	Description  string    `json:"description,omitempty"`
	Genres       []string  `json:"genres,omitempty"`
	Status       string    `json:"status"`
	ThumbnailURL string    `json:"thumbnailUrl,omitempty"`
	Favorite     bool      `json:"favorite"`
	AddedAt      time.Time `json:"addedAt,omitzero"`
	Categories   []string  `json:"categories,omitempty"`
	// Trackers maps a tracker (TrackerAniList, ...) to the series id there.
	Trackers           map[string]string `json:"trackers,omitempty"`
	ExcludedScanlators []string          `json:"excludedScanlators,omitempty"`
	Chapters           []Chapter         `json:"chapters,omitempty"`
}

// Chapter is one chapter of an entry with its read state.
type Chapter struct {
	URL       string  `json:"url"`
	Name      string  `json:"name,omitempty"`
	Scanlator string  `json:"scanlator,omitempty"`
	Lang      string  `json:"lang,omitempty"`
	Number    float64 `json:"number"` // -1 when unknown
	Read      bool    `json:"read,omitempty"`
	// LastPageRead is the 0-based page reached (0 when not started).
	LastPageRead int        `json:"lastPageRead,omitempty"`
	ReadAt       *time.Time `json:"readAt,omitempty"`
}

// ReadCount returns how many chapters are read.
func (e Entry) ReadCount() int {
	n := 0
	for _, c := range e.Chapters {
		if c.Read {
			n++
		}
	}
	return n
}

// ErrLegacyJSON is returned for Tachiyomi's old JSON backups.
var ErrLegacyJSON = errors.New("this is an old Tachiyomi JSON backup; restore it in Mihon and create a new backup")

// ErrUnknownFormat is returned for files that aren't a supported backup.
var ErrUnknownFormat = errors.New("not a Mihon, Tachiyomi, Suwayomi or Aidoku backup")

// MaxSize is the largest backup accepted (decompressed).
const MaxSize = 256 << 20

// Parse detects the format and parses a backup.
func Parse(data []byte) (*Backup, error) {
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("gzip: %w", err)
		}
		raw, err := io.ReadAll(io.LimitReader(zr, MaxSize+1))
		if err != nil {
			return nil, fmt.Errorf("gzip: %w", err)
		}
		if len(raw) > MaxSize {
			return nil, errors.New("backup is too large")
		}
		data = raw
	}
	switch {
	case bytes.HasPrefix(data, []byte("bplist00")):
		return parseAidoku(data)
	case bytes.HasPrefix(data, []byte("<?xml")):
		return parseAidoku(data)
	case len(bytes.TrimSpace(data)) > 0 && bytes.TrimSpace(data)[0] == '{':
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(data, &probe); err != nil {
			return nil, ErrUnknownFormat
		}
		if _, ok := probe["mangas"]; ok {
			return nil, ErrLegacyJSON
		}
		if _, ok := probe["version"]; ok && probe["library"] == nil && probe["manga"] == nil {
			return nil, ErrLegacyJSON
		}
		return parseAidokuJSON(data)
	case len(data) > 0 && data[0] == 0x0a: // field 1 (backupManga), length-delimited
		return parseMihon(data)
	case len(data) == 0:
		return nil, errors.New("the backup is empty")
	}
	return nil, ErrUnknownFormat
}
