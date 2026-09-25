package db

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

func TestMigrationsForward(t *testing.T) {
	for _, dialect := range []Dialect{SQLite, Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d := migrationTestDB(t, dialect)
			ctx := context.Background()

			if err := d.Migrate(ctx); err != nil {
				t.Fatalf("migrate empty database: %v", err)
			}
			version, err := goose.GetDBVersionContext(ctx, d.DB.DB)
			if err != nil {
				t.Fatal(err)
			}
			if want := latestMigrationVersion(t, d); version != want {
				t.Fatalf("database version is %d, want %d", version, want)
			}
			if err := d.Migrate(ctx); err != nil {
				t.Fatalf("repeat migration: %v", err)
			}
			repeatedVersion, err := goose.GetDBVersionContext(ctx, d.DB.DB)
			if err != nil {
				t.Fatal(err)
			}
			if repeatedVersion != version {
				t.Fatalf("repeat migration changed version from %d to %d", version, repeatedVersion)
			}
			results, err := migrationProvider(t, d).Up(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 0 {
				t.Fatalf("repeat migration applied %d migrations, want no-op", len(results))
			}
		})
	}
}

func TestMigrationsPreserveDataFromEarlierVersions(t *testing.T) {
	for _, dialect := range []Dialect{SQLite, Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d := migrationTestDB(t, dialect)
			p := migrationProvider(t, d)
			ctx := context.Background()
			migrateTo := func(version int64) {
				t.Helper()
				if _, err := p.UpTo(ctx, version); err != nil {
					t.Fatalf("migrate to %d: %v", version, err)
				}
			}

			migrateTo(1)
			for _, q := range []string{
				`INSERT INTO root_folders (id, path, language, created_at) VALUES (1, '/library/sample', 'en', '2024-01-02T03:04:05Z')`,
				`INSERT INTO profiles (id, name, created_at, updated_at) VALUES (1, 'Default', '2024-01-02T03:04:05Z', '2024-01-02T03:04:05Z')`,
				`INSERT INTO series (id, title, sort_title, root_folder_id, path, profile_id, added_at, updated_at) VALUES (1, 'Sample Series', 'sample series', 1, 'sample', 1, '2024-01-02T03:04:05Z', '2024-01-02T03:04:05Z')`,
				`INSERT INTO chapters (id, series_id, number_key, number_sort, title, first_seen_at, updated_at) VALUES (1, 1, '1', 1, 'First Chapter', '2024-01-02T03:04:05Z', '2024-01-02T03:04:05Z')`,
			} {
				if _, err := d.ExecContext(ctx, q); err != nil {
					t.Fatalf("seed version 1: %v\n%s", err, q)
				}
			}

			migrateTo(10)
			if _, err := d.ExecContext(ctx, `INSERT INTO users (id, username, password_hash, created_at) VALUES (1, 'sample-user', 'hash-placeholder', '2024-01-02T03:04:05Z')`); err != nil {
				t.Fatalf("seed version 10: %v", err)
			}
			migrateTo(17)
			if _, err := d.ExecContext(ctx, `INSERT INTO works (id, title, sort_title, created_at, updated_at) VALUES (99, 'Later Work', 'later work', '2024-02-03T04:05:06Z', '2024-02-03T04:05:06Z')`); err != nil {
				t.Fatalf("seed version 17: %v", err)
			}
			migrateTo(24)
			if _, err := p.Up(ctx); err != nil {
				t.Fatalf("migrate to latest: %v", err)
			}

			var title, workTitle, username string
			var workID int64
			if err := d.QueryRowContext(ctx, `SELECT title, work_id FROM series WHERE id = 1`).Scan(&title, &workID); err != nil {
				t.Fatal(err)
			}
			if title != "Sample Series" || workID != 1 {
				t.Fatalf("series was not preserved and migrated: title=%q work_id=%d", title, workID)
			}
			if err := d.QueryRowContext(ctx, `SELECT title FROM works WHERE id = 99`).Scan(&workTitle); err != nil || workTitle != "Later Work" {
				t.Fatalf("intermediate work was not preserved: title=%q err=%v", workTitle, err)
			}
			if err := d.QueryRowContext(ctx, `SELECT username FROM users WHERE id = 1`).Scan(&username); err != nil || username != "sample-user" {
				t.Fatalf("intermediate user was not preserved: username=%q err=%v", username, err)
			}
			var chapterTitle string
			if err := d.QueryRowContext(ctx, `SELECT title FROM chapters WHERE id = 1`).Scan(&chapterTitle); err != nil || chapterTitle != "First Chapter" {
				t.Fatalf("chapter was not preserved: title=%q err=%v", chapterTitle, err)
			}

			fresh := migrationTestDB(t, dialect)
			if err := fresh.Migrate(ctx); err != nil {
				t.Fatalf("migrate fresh comparison database: %v", err)
			}
			if got, want := migrationSchema(t, d), migrationSchema(t, fresh); fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("stepwise schema differs from fresh schema\nstepwise: %v\nfresh: %v", got, want)
			}
		})
	}
}

func TestFinalMigrationSchemasAgree(t *testing.T) {
	ctx := context.Background()
	sqliteDB := migrationTestDB(t, SQLite)
	pgDB := migrationTestDB(t, Postgres)
	for _, d := range []*DB{sqliteDB, pgDB} {
		if err := d.Migrate(ctx); err != nil {
			t.Fatalf("migrate %s: %v", d.Kind, err)
		}
	}
	sqliteSchema := migrationSchema(t, sqliteDB)
	pgSchema := migrationSchema(t, pgDB)
	if fmt.Sprint(sqliteSchema) != fmt.Sprint(pgSchema) {
		t.Fatalf("SQLite and PostgreSQL schemas differ\nSQLite: %v\nPostgres: %v", sqliteSchema, pgSchema)
	}
}

func migrationTestDB(t *testing.T, dialect Dialect) *DB {
	t.Helper()
	ctx := context.Background()
	if dialect == SQLite {
		d, err := Open(ctx, "sqlite://"+filepath.Join(t.TempDir(), "migrations.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = d.Close() })
		return d
	}
	base := os.Getenv("MANGARR_TEST_POSTGRES")
	if base == "" {
		t.Skip("MANGARR_TEST_POSTGRES not set")
	}
	admin, err := Open(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("mangarr_migrations_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	d, err := Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = d.Close()
		_, _ = admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		_ = admin.Close()
	})
	return d
}

func migrationProvider(t *testing.T, d *DB) *goose.Provider {
	t.Helper()
	sub, err := migrationsFSSub(d.Kind)
	if err != nil {
		t.Fatal(err)
	}
	dialect := goose.DialectSQLite3
	if d.Kind == Postgres {
		dialect = goose.DialectPostgres
	}
	p, err := goose.NewProvider(dialect, d.DB.DB, sub)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// latestMigrationVersion is the highest version shipped for d's dialect, so
// the test keeps passing as migrations are added.
func latestMigrationVersion(t *testing.T, d *DB) int64 {
	t.Helper()
	sources := migrationProvider(t, d).ListSources()
	if len(sources) == 0 {
		t.Fatal("no migrations found")
	}
	return sources[len(sources)-1].Version
}

func migrationsFSSub(dialect Dialect) (fs.FS, error) {
	return fs.Sub(migrationsFS, "migrations/"+string(dialect))
}

type schemaColumn struct {
	Table    string
	Column   string
	Nullable bool
}

func migrationSchema(t *testing.T, d *DB) []schemaColumn {
	t.Helper()
	ctx := context.Background()
	var out []schemaColumn
	if d.Kind == SQLite {
		rows, err := d.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT IN ('sqlite_sequence', 'goose_db_version') ORDER BY name`)
		if err != nil {
			t.Fatal(err)
		}
		var tables []string
		for rows.Next() {
			var table string
			if err := rows.Scan(&table); err != nil {
				t.Fatal(err)
			}
			tables = append(tables, table)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		for _, table := range tables {
			cols, err := d.QueryContext(ctx, `SELECT name, "notnull", pk FROM pragma_table_info(?)`, table)
			if err != nil {
				t.Fatal(err)
			}
			for cols.Next() {
				var name string
				var notNull, pk int
				if err := cols.Scan(&name, &notNull, &pk); err != nil {
					t.Fatal(err)
				}
				out = append(out, schemaColumn{Table: table, Column: name, Nullable: notNull == 0 && pk == 0})
			}
			if err := cols.Close(); err != nil {
				t.Fatal(err)
			}
		}
	} else {
		rows, err := d.QueryContext(ctx, `SELECT table_name, column_name, is_nullable FROM information_schema.columns WHERE table_schema = current_schema() AND table_name <> 'goose_db_version' ORDER BY table_name, ordinal_position`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var table, column, nullable string
			if err := rows.Scan(&table, &column, &nullable); err != nil {
				t.Fatal(err)
			}
			out = append(out, schemaColumn{Table: table, Column: column, Nullable: nullable == "YES"})
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Table == out[j].Table {
			return out[i].Column < out[j].Column
		}
		return out[i].Table < out[j].Table
	})
	return out
}

func TestQueueRankBackfill(t *testing.T) {
	for _, dialect := range []Dialect{SQLite, Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d := migrationTestDB(t, dialect)
			p := migrationProvider(t, d)
			ctx := t.Context()
			if _, err := p.UpTo(ctx, 26); err != nil {
				t.Fatal(err)
			}
			for _, query := range []string{
				`INSERT INTO root_folders (id,path,created_at) VALUES (1,'/library/copper','2024-01-02T03:04:05Z')`,
				`INSERT INTO profiles (id,name,created_at,updated_at) VALUES (1,'Queue profile','2024-01-02T03:04:05Z','2024-01-02T03:04:05Z')`,
				`INSERT INTO series (id,title,sort_title,root_folder_id,path,profile_id,added_at,updated_at) VALUES (1,'Copper Clouds','copper clouds',1,'copper',1,'2024-01-02T03:04:05Z','2024-01-02T03:04:05Z')`,
			} {
				if _, err := d.ExecContext(ctx, query); err != nil {
					t.Fatal(err)
				}
			}
			priorities := []int{0, 100, 0, -100, 100, -100}
			statuses := []string{"queued", "paused", "downloading", "processing", "failed", "completed"}
			for i, priority := range priorities {
				if _, err := d.ExecContext(ctx, `INSERT INTO chapters (id,series_id,number_key,number_sort,first_seen_at,updated_at) VALUES (?,1,?,?,'2024-01-02T03:04:05Z','2024-01-02T03:04:05Z')`, i+1, fmt.Sprint(i+1), i+1); err != nil {
					t.Fatal(err)
				}
				kind := "download"
				if i%2 == 1 {
					kind = "reprocess"
				}
				if _, err := d.ExecContext(ctx, `INSERT INTO download_jobs (id,series_id,chapter_id,kind,status,priority,not_before,created_at,updated_at) VALUES (?,1,?,?,?,?,'2024-01-02T03:04:05Z','2024-01-02T03:04:05Z','2024-01-02T03:04:05Z')`, i+1, i+1, kind, statuses[i], priority); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := p.Up(ctx); err != nil {
				t.Fatal(err)
			}
			type ranked struct {
				ID       int64
				Rank     int64
				Priority int
				Status   string
			}
			var jobs []ranked
			if err := d.NewSelect().Table("download_jobs").Column("id", "rank", "priority", "status").Order("rank").Scan(ctx, &jobs); err != nil {
				t.Fatal(err)
			}
			want := []int64{2, 5, 1, 3, 4, 6}
			if len(jobs) != len(want) {
				t.Fatal(jobs)
			}
			for i, job := range jobs {
				if job.ID != want[i] || job.Rank != int64(i+1)*1048576 || job.Status != statuses[job.ID-1] || job.Priority != priorities[job.ID-1] {
					t.Fatalf("backfill changed ordering or data: %+v", jobs)
				}
			}
			if err := d.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			var again []ranked
			if err := d.NewSelect().Table("download_jobs").Column("id", "rank", "priority", "status").Order("rank").Scan(ctx, &again); err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(again) != fmt.Sprint(jobs) {
				t.Fatal("repeat migration changed ranks")
			}
		})
	}
}
