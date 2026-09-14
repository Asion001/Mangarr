package organize

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/events"
	"github.com/Asion001/mangarr/internal/library"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/settings"
)

func setup(t *testing.T) (*Service, *model.Series, string, *model.RootFolder) {
	t.Helper()
	d := dbtest.SQLite(t)
	ctx := context.Background()
	st := settings.NewStore(d)
	_ = st.Warm(ctx)
	lib := library.New(d, st, nil, t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc := New(d, lib, events.NewBus(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Now().UTC()
	r1 := &model.RootFolder{Path: filepath.Join(t.TempDir(), "a"), CreatedAt: now}
	r2 := &model.RootFolder{Path: filepath.Join(t.TempDir(), "b"), CreatedAt: now}
	for _, rf := range []*model.RootFolder{r1, r2} {
		_ = os.MkdirAll(rf.Path, 0o755)
		if _, err := d.NewInsert().Model(rf).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	p := &model.Profile{Name: "P", IsDefault: true, CreatedAt: now, UpdatedAt: now}
	_, _ = d.NewInsert().Model(p).Exec(ctx)
	ser := &model.Series{Title: "S", SortTitle: "s", RootFolderID: r1.ID, Path: "S", ProfileID: p.ID, Tags: []int64{}, AddedAt: now, UpdatedAt: now}
	if _, err := d.NewInsert().Model(ser).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(r1.Path, "S")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "S Ch.0001.cbz"), []byte("zipdata"), 0o644)
	ch := &model.Chapter{SeriesID: ser.ID, NumberKey: "1", NumberSort: 1, State: model.ChapterImported, FirstSeenAt: now, UpdatedAt: now}
	_, _ = d.NewInsert().Model(ch).Exec(ctx)
	f := &model.ChapterFile{ChapterID: ch.ID, SeriesID: ser.ID, RelativePath: "S Ch.0001.cbz", Size: 7, ImportedAt: now}
	_, _ = d.NewInsert().Model(f).Exec(ctx)
	return svc, ser, dir, r2
}

func TestMoveAcrossFileSystems(t *testing.T) {
	svc, ser, oldDir, r2 := setup(t)
	orig := rename
	defer func() { rename = orig }()
	rename = func(string, string) error { return errors.New("invalid cross-device link") }
	if err := svc.Move(context.Background(), MoveRequest{SeriesID: ser.ID, RootFolderID: r2.ID, MoveFiles: true}); err != nil {
		t.Fatal(err)
	}
	newDir := filepath.Join(r2.Path, "S")
	if data, err := os.ReadFile(filepath.Join(newDir, "S Ch.0001.cbz")); err != nil || string(data) != "zipdata" {
		t.Fatalf("copied file: %q %v", data, err)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatal("old folder should be removed after the copy")
	}
	if _, err := os.Stat(filepath.Join(newDir, MarkerName)); !os.IsNotExist(err) {
		t.Fatal("marker should be removed")
	}
	// moving into a non-empty folder is refused
	if err := svc.Move(context.Background(), MoveRequest{SeriesID: ser.ID, Path: "..", MoveFiles: true}); err == nil {
		t.Fatal("invalid path must be rejected")
	}
}

func TestResumeMoves(t *testing.T) {
	svc, ser, oldDir, r2 := setup(t)
	ctx := context.Background()
	// crash after the copy, before mangarr pointed at it: the partial copy goes
	partial := filepath.Join(r2.Path, "S")
	_ = os.MkdirAll(partial, 0o755)
	_ = os.WriteFile(filepath.Join(partial, MarkerName), []byte(oldDir), 0o644)
	if err := svc.ResumeMoves(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatal("partial copy should be removed")
	}
	if _, err := os.Stat(oldDir); err != nil {
		t.Fatal("original must stay")
	}
	// crash after the database update: the old folder goes
	_ = os.MkdirAll(partial, 0o755)
	_ = os.WriteFile(filepath.Join(partial, MarkerName), []byte(oldDir), 0o644)
	_, _ = svc.db.NewUpdate().Model((*model.Series)(nil)).Set("root_folder_id = ?", r2.ID).Where("id = ?", ser.ID).Exec(ctx)
	if err := svc.ResumeMoves(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatal("old folder should be removed once mangarr points at the copy")
	}
	if _, err := os.Stat(filepath.Join(partial, MarkerName)); !os.IsNotExist(err) {
		t.Fatal("marker should be removed")
	}
}
