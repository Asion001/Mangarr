// Package db opens the configured database (SQLite or PostgreSQL) through bun
// and applies embedded, per-dialect goose migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/pressly/goose/v3"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	_ "modernc.org/sqlite"
)

//go:embed migrations
var migrationsFS embed.FS

type Dialect string

const (
	SQLite   Dialect = "sqlite"
	Postgres Dialect = "postgres"
)

type DB struct {
	*bun.DB
	Kind Dialect
	// Path is the SQLite file path (empty for Postgres).
	Path string
}

// Open parses a DSN of the form "sqlite:///path/file.db" or
// "postgres://user:pass@host:5432/db?sslmode=disable".
func Open(ctx context.Context, dsn string) (*DB, error) {
	switch {
	case strings.HasPrefix(dsn, "sqlite://"):
		return openSQLite(ctx, strings.TrimPrefix(dsn, "sqlite://"))
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		return openPostgres(ctx, dsn)
	default:
		return nil, fmt.Errorf("unsupported database DSN %q (use sqlite:// or postgres://)", redact(dsn))
	}
}

func openSQLite(ctx context.Context, path string) (*DB, error) {
	if path == ":memory:" {
		path = "file::memory:?cache=shared"
	} else if err := os.MkdirAll(filepath.Dir(path), 0o775); err != nil {
		return nil, err
	}
	q := url.Values{}
	for _, p := range []string{"busy_timeout(15000)", "journal_mode(WAL)", "foreign_keys(1)", "synchronous(NORMAL)"} {
		q.Add("_pragma", p)
	}
	q.Set("_txlock", "immediate")
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	sqldb, err := sql.Open("sqlite", path+sep+q.Encode())
	if err != nil {
		return nil, err
	}
	// WAL allows concurrent readers; writers serialize through BEGIN IMMEDIATE
	// and busy_timeout. Keep the pool small: the workload is light.
	sqldb.SetMaxOpenConns(4)
	if err := sqldb.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	return &DB{DB: bun.NewDB(sqldb, sqlitedialect.New()), Kind: SQLite, Path: path}, nil
}

func openPostgres(ctx context.Context, dsn string) (*DB, error) {
	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	sqldb.SetMaxOpenConns(20)
	if err := sqldb.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("open postgres %s: %w", redact(dsn), err)
	}
	return &DB{DB: bun.NewDB(sqldb, pgdialect.New()), Kind: Postgres}, nil
}

// Migrate applies all pending migrations for the active dialect.
func (d *DB) Migrate(ctx context.Context) error {
	sub, err := fs.Sub(migrationsFS, "migrations/"+string(d.Kind))
	if err != nil {
		return err
	}
	gd := goose.DialectSQLite3
	if d.Kind == Postgres {
		gd = goose.DialectPostgres
	}
	p, err := goose.NewProvider(gd, d.DB.DB, sub)
	if err != nil {
		return err
	}
	res, err := p.Up(ctx)
	for _, r := range res {
		slog.Info("applied migration", "version", r.Source.Version, "path", r.Source.Path, "duration", r.Duration)
	}
	return err
}

// Backup writes a consistent copy of an SQLite database to dst. Postgres
// deployments should use pg_dump instead.
func (d *DB) Backup(ctx context.Context, dst string) error {
	if d.Kind != SQLite {
		return fmt.Errorf("database backup is only built in for SQLite; use pg_dump for Postgres")
	}
	_ = os.Remove(dst)
	_, err := d.ExecContext(ctx, "VACUUM INTO ?", dst)
	return err
}

func redact(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return "<invalid>"
	}
	if u.User != nil {
		u.User = url.UserPassword(u.User.Username(), "***")
	}
	return u.String()
}
