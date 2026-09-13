// Package dbtest provides test databases. SQLite always runs; Postgres runs
// when MANGARR_TEST_POSTGRES is set to a DSN of a disposable database server
// (each test gets its own freshly created database).
package dbtest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/db"
)

var counter atomic.Int64

// SQLite returns a migrated SQLite database in a temp dir.
func SQLite(t testing.TB) *db.DB {
	t.Helper()
	d, err := db.Open(context.Background(), "sqlite://"+filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// Postgres returns a migrated database on the server in MANGARR_TEST_POSTGRES,
// or skips the test when it is unset.
func Postgres(t testing.TB) *db.DB {
	t.Helper()
	base := os.Getenv("MANGARR_TEST_POSTGRES")
	if base == "" {
		t.Skip("MANGARR_TEST_POSTGRES not set")
	}
	ctx := context.Background()
	admin, err := db.Open(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("mangarr_test_%d_%d", time.Now().UnixNano()%1e9, counter.Add(1))
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(base)
	u.Path = "/" + name
	d, err := db.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = d.Close()
		_, _ = admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		_ = admin.Close()
	})
	return d
}

// ForEachDialect runs fn as a subtest against SQLite and (if configured) Postgres.
func ForEachDialect(t *testing.T, fn func(t *testing.T, d *db.DB)) {
	t.Run("sqlite", func(t *testing.T) { fn(t, SQLite(t)) })
	t.Run("postgres", func(t *testing.T) { fn(t, Postgres(t)) })
}

// Name returns a sanitized name for logs.
func Name(d *db.DB) string { return strings.ToLower(string(d.Dialect)) }
