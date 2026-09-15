package model

import (
	"time"

	"github.com/uptrace/bun"
)

// ReadingKey is an API key a reading app uses with the Komga-compatible API.
// Only a hash is stored; Prefix identifies the key in the UI.
type ReadingKey struct {
	bun.BaseModel `bun:"table:reading_keys"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	KeyHash       string `bun:"key_hash,notnull" json:"-"`
	Prefix        string `bun:"prefix,notnull" json:"prefix"`
	// Comment names the device ("KMReader iPad").
	Comment string `bun:"comment,notnull" json:"comment"`
	// LastClient is the app that last used the key (from its User-Agent).
	LastClient string     `bun:"last_client,notnull" json:"lastClient"`
	CreatedAt  time.Time  `bun:"created_at,notnull" json:"createdAt"`
	LastUsedAt *time.Time `bun:"last_used_at" json:"lastUsedAt,omitempty"`
}

// ReadOriginApp marks read states written by reading apps through the
// Komga-compatible API (kept until a library server reports the chapter,
// like ReadOriginBackup).
const ReadOriginApp = "app"

// Read event origins (ReadEvent.Origin).
const (
	EventOriginApp    = "app"    // a reading app through the Komga-compatible API
	EventOriginServer = "server" // a library server (Komga, Kavita)
	EventOriginBackup = "backup" // an imported backup
)

// Read event outcomes.
const (
	OutcomeApplied   = "applied"
	OutcomeKept      = "kept"      // lower than mangarr's state: mangarr kept its own
	OutcomeUnread    = "unread"    // explicitly marked unread
	OutcomeUnchanged = "unchanged" // nothing new
)

// ReadEvent is one progress report, for sync health per device.
type ReadEvent struct {
	bun.BaseModel `bun:"table:read_events"`
	ID            int64     `bun:"id,pk,autoincrement" json:"id"`
	ReaderID      int64     `bun:"reader_id,notnull" json:"readerId"`
	SeriesID      int64     `bun:"series_id,notnull" json:"seriesId"`
	ChapterID     int64     `bun:"chapter_id,notnull" json:"chapterId"`
	Completed     bool      `bun:"completed,notnull" json:"completed"`
	Page          int       `bun:"page,notnull" json:"page"`
	Origin        string    `bun:"origin,notnull" json:"origin"`
	Client        string    `bun:"client,notnull" json:"client"`
	Device        string    `bun:"device,notnull" json:"device"`
	Outcome       string    `bun:"outcome,notnull" json:"outcome"`
	At            time.Time `bun:"at,notnull" json:"at"`
}
