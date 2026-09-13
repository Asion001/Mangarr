// Package settings stores runtime-editable global settings as JSON documents
// in the settings table, with typed accessors and defaults.
package settings

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/model"
)

// General holds server-wide settings.
type General struct {
	APIKey        string `json:"apiKey"`
	SessionSecret string `json:"sessionSecret"`
	// InstanceName is shown in the UI and notifications.
	InstanceName string `json:"instanceName"`
	// PublicURL is used in notification links (e.g. https://mangarr.example.com).
	PublicURL string `json:"publicUrl"`
	// BackupRetention is the number of scheduled backups to keep.
	BackupRetention int `json:"backupRetention"`
}

// MediaManagement controls file naming and import behavior.
type MediaManagement struct {
	// ChapterFormat is the file name template (without extension).
	ChapterFormat string `json:"chapterFormat"`
	// SeriesFolderFormat is the folder name template for new series.
	SeriesFolderFormat string `json:"seriesFolderFormat"`
	// RecycleBinPath receives replaced/cleaned files; empty = <dataDir>/recycle.
	RecycleBinPath string `json:"recycleBinPath"`
	// RecycleBinDays purges recycled files older than N days (0 = never).
	RecycleBinDays int `json:"recycleBinDays"`
	// MinFreeSpaceMB aborts downloads when a root folder has less free space.
	MinFreeSpaceMB int `json:"minFreeSpaceMb"`
	// WriteSeriesJSON writes a Mylar-style series.json (read by Komga).
	WriteSeriesJSON bool `json:"writeSeriesJson"`
	// WriteCover writes cover.jpg into series folders.
	WriteCover bool `json:"writeCover"`
	// WriteVolume writes <Volume> into ComicInfo.xml. Off by default: volume
	// numbers change Kavita's grouping and Komga's default series titles.
	WriteVolume bool `json:"writeVolume"`
	// FileMode / DirMode for created files (octal strings like "0664").
	FileMode string `json:"fileMode"`
	DirMode  string `json:"dirMode"`
}

// Downloads controls queue behavior.
type Downloads struct {
	// MaxConcurrent is the number of chapters downloaded in parallel (global).
	MaxConcurrent int `json:"maxConcurrent"`
	// MaxPerSource is the number of chapters downloaded in parallel per source.
	MaxPerSource int `json:"maxPerSource"`
	// PageConcurrency is the number of pages fetched in parallel within a chapter.
	PageConcurrency int `json:"pageConcurrency"`
	// PageRetries per page before the chapter attempt fails.
	PageRetries int `json:"pageRetries"`
	// MaxAttempts per release before it is blocklisted and the next source is tried.
	MaxAttempts int `json:"maxAttempts"`
	// DefaultCheckIntervalMinutes for ongoing series.
	DefaultCheckIntervalMinutes int `json:"defaultCheckIntervalMinutes"`
}

// Cleanup holds global read-based cleanup rules (off by default).
type Cleanup struct {
	Enabled bool `json:"enabled"`
	DryRun  bool `json:"dryRun"`
	// Statuses the rule applies to ("ongoing" by default).
	Statuses []string `json:"statuses"`
	// ReaderIDs restricts the required readers (empty = all readers counting for cleanup).
	ReaderIDs               []int64  `json:"readerIds"`
	IgnoreReadersNotStarted bool     `json:"ignoreReadersNotStarted"`
	KeepLastRead            int      `json:"keepLastRead"`
	GraceDays               int      `json:"graceDays"`
	MinFreeSpaceGB          int      `json:"minFreeSpaceGb"`
	ExcludeTags             []string `json:"excludeTags"`
	UseRecycleBin           bool     `json:"useRecycleBin"`
}

// ReadSync controls progress polling from library servers.
type ReadSync struct {
	IntervalMinutes int `json:"intervalMinutes"`
}

func DefaultGeneral() General {
	return General{InstanceName: "mangarr", BackupRetention: 7}
}

func DefaultMediaManagement() MediaManagement {
	return MediaManagement{
		ChapterFormat:      "{Series Title} Ch.{Chapter:0000}",
		SeriesFolderFormat: "{Series Title}",
		RecycleBinDays:     7,
		MinFreeSpaceMB:     1024,
		WriteSeriesJSON:    true,
		WriteCover:         true,
		FileMode:           "0664",
		DirMode:            "0775",
	}
}

func DefaultDownloads() Downloads {
	return Downloads{MaxConcurrent: 3, MaxPerSource: 1, PageConcurrency: 3, PageRetries: 3, MaxAttempts: 3, DefaultCheckIntervalMinutes: 360}
}

func DefaultCleanup() Cleanup {
	return Cleanup{
		Enabled: false, DryRun: true, Statuses: []string{model.StatusOngoing},
		IgnoreReadersNotStarted: true, KeepLastRead: 1, GraceDays: 7,
		ExcludeTags: []string{"keep"}, UseRecycleBin: true,
	}
}

func DefaultReadSync() ReadSync { return ReadSync{IntervalMinutes: 30} }

// Store caches settings documents in memory.
type Store struct {
	db    *db.DB
	mu    sync.RWMutex
	cache map[string]json.RawMessage
}

func NewStore(d *db.DB) *Store { return &Store{db: d, cache: map[string]json.RawMessage{}} }

const (
	KeyGeneral         = "general"
	KeyMediaManagement = "media_management"
	KeyDownloads       = "downloads"
	KeyCleanup         = "cleanup"
	KeyReadSync        = "read_sync"
)

// Get decodes the document at key into out (which must hold defaults).
func (s *Store) Get(ctx context.Context, key string, out any) error {
	s.mu.RLock()
	raw, ok := s.cache[key]
	s.mu.RUnlock()
	if !ok {
		var row model.Setting
		err := s.db.NewSelect().Model(&row).Where("key = ?", key).Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			// remember "not stored" so defaults don't cost a query each time
			s.mu.Lock()
			s.cache[key] = json.RawMessage("null")
			s.mu.Unlock()
			return nil
		}
		if err != nil {
			return err
		}
		raw = json.RawMessage(row.Value)
		s.mu.Lock()
		s.cache[key] = raw
		s.mu.Unlock()
	}
	return json.Unmarshal(raw, out)
}

// Set stores v at key.
func (s *Store) Set(ctx context.Context, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	row := &model.Setting{Key: key, Value: string(b), UpdatedAt: time.Now().UTC()}
	_, err = s.db.NewInsert().Model(row).
		On("CONFLICT (key) DO UPDATE").
		Set("value = EXCLUDED.value").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cache[key] = b
	s.mu.Unlock()
	return nil
}

func (s *Store) General(ctx context.Context) (General, error) {
	v := DefaultGeneral()
	return v, s.Get(ctx, KeyGeneral, &v)
}

func (s *Store) MediaManagement(ctx context.Context) (MediaManagement, error) {
	v := DefaultMediaManagement()
	return v, s.Get(ctx, KeyMediaManagement, &v)
}

func (s *Store) Downloads(ctx context.Context) (Downloads, error) {
	v := DefaultDownloads()
	return v, s.Get(ctx, KeyDownloads, &v)
}

func (s *Store) Cleanup(ctx context.Context) (Cleanup, error) {
	v := DefaultCleanup()
	return v, s.Get(ctx, KeyCleanup, &v)
}

func (s *Store) ReadSync(ctx context.Context) (ReadSync, error) {
	v := DefaultReadSync()
	return v, s.Get(ctx, KeyReadSync, &v)
}

// EnsureSecrets generates the API key and session secret on first start.
func (s *Store) EnsureSecrets(ctx context.Context) (General, error) {
	g, err := s.General(ctx)
	if err != nil {
		return g, err
	}
	changed := false
	if g.APIKey == "" {
		g.APIKey = RandomHex(16)
		changed = true
	}
	if g.SessionSecret == "" {
		g.SessionSecret = RandomHex(32)
		changed = true
	}
	if changed {
		return g, s.Set(ctx, KeyGeneral, g)
	}
	return g, nil
}

func RandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
