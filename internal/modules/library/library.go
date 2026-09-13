// Package library defines library-server modules (Komga, Kavita): servers
// that serve our files to reader apps. mangarr asks them to rescan after
// imports and (optionally) reads per-user progress for read-based cleanup.
package library

import (
	"context"
	"time"

	"github.com/Asion001/mangarr/internal/modules"
)

type Module interface {
	modules.Instance
	// Rescan asks the server to pick up changes in the given local series
	// folders (absolute paths as seen by mangarr; path mapping is the module's job).
	Rescan(ctx context.Context, localPaths []string) error
}

// Account holds per-user credentials for a library server.
type Account struct {
	Credentials map[string]string
}

// BookProgress is a user's progress on one file.
type BookProgress struct {
	LocalPath string     `json:"localPath"`
	Completed bool       `json:"completed"`
	Page      int        `json:"page"`
	ReadAt    *time.Time `json:"readAt,omitempty"`
}

// ProgressReader is implemented by servers that expose per-user read progress.
type ProgressReader interface {
	// AccountFields describes the credentials a reader must supply.
	AccountFields() []modules.Field
	// TestAccount validates credentials and returns the server-side username.
	TestAccount(ctx context.Context, acc Account) (string, error)
	// ReadProgress returns progress for all books under localRoots that the
	// user has started or finished.
	ReadProgress(ctx context.Context, acc Account, localRoots []string) ([]BookProgress, error)
}
