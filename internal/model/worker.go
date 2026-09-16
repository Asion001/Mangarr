package model

import (
	"time"

	"github.com/uptrace/bun"
)

// Worker roles. A worker may hold several: the all-in-one image can be a
// downloader and an encoder at once.
const (
	// RoleDownload fetches a chapter's pages and uploads them here, which
	// spreads downloading across machines (and addresses).
	RoleDownload = "download"
	// RoleUpscale runs an upscaler on pages.
	RoleUpscale = "upscale"
	// RoleEncode re-encodes pages (AVIF, JPEG XL).
	RoleEncode = "encode"
)

// WorkerRoles are the roles a worker can be given, in the order the UI
// shows them.
var WorkerRoles = []string{RoleDownload, RoleUpscale, RoleEncode}

// Worker is a machine that asks this server for work. It holds a key of its
// own, so it can be switched off or removed without touching anything else,
// and its counters are what System → Workers shows.
type Worker struct {
	bun.BaseModel `bun:"table:workers"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Name          string `bun:"name,notnull" json:"name"`
	KeyHash       string `bun:"key_hash,notnull" json:"-"`
	// Prefix is the start of the key, so a worker can be told apart in the UI.
	Prefix  string   `bun:"prefix,notnull" json:"prefix"`
	Roles   []string `bun:"roles,type:jsonb,notnull" json:"roles"`
	Enabled bool     `bun:"enabled,notnull" json:"enabled"`
	// Version, Platform and Info are what the worker said about itself when
	// it last said hello (its build, its OS, its upscaling devices).
	Version  string         `bun:"version,notnull" json:"version"`
	Platform string         `bun:"platform,notnull" json:"platform"`
	Info     map[string]any `bun:"info,type:jsonb,notnull" json:"info"`
	LastIP   string         `bun:"last_ip,notnull" json:"lastIp"`
	// CreatedBy is the account that made the key (0 when it came from the
	// admin API key).
	CreatedBy   int64      `bun:"created_by,nullzero" json:"createdBy,omitempty"`
	CreatedAt   time.Time  `bun:"created_at,notnull" json:"createdAt"`
	LastSeenAt  *time.Time `bun:"last_seen_at" json:"lastSeenAt,omitempty"`
	TasksDone   int64      `bun:"tasks_done,notnull" json:"tasksDone"`
	TasksFailed int64      `bun:"tasks_failed,notnull" json:"tasksFailed"`
	PagesDone   int64      `bun:"pages_done,notnull" json:"pagesDone"`
	BytesIn     int64      `bun:"bytes_in,notnull" json:"bytesIn"`
	BytesOut    int64      `bun:"bytes_out,notnull" json:"bytesOut"`
	BusySeconds float64    `bun:"busy_seconds,notnull" json:"busySeconds"`
}

// HasRole reports whether the worker may do this kind of work.
func (w *Worker) HasRole(role string) bool {
	for _, r := range w.Roles {
		if r == role {
			return true
		}
	}
	return false
}
