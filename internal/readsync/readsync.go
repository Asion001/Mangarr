// Package readsync pulls per-reader progress from library modules (Komga,
// Kavita) and stores it as chapter read states.
package readsync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/uptrace/bun"

	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/events"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/library"
)

type Syncer struct {
	db   *db.DB
	mods *modules.Manager
	bus  *events.Bus
	log  *slog.Logger
}

func New(d *db.DB, mods *modules.Manager, bus *events.Bus, log *slog.Logger) *Syncer {
	return &Syncer{db: d, mods: mods, bus: bus, log: log}
}

type Result struct {
	Accounts int `json:"accounts"`
	Updated  int `json:"updated"`
	Failed   int `json:"failed"`
}

type fileRef struct {
	chapterID int64
	seriesID  int64
}

// index maps absolute file paths to chapters.
func (s *Syncer) index(ctx context.Context) (map[string]fileRef, []string, error) {
	var rows []struct {
		ChapterID    int64  `bun:"chapter_id"`
		SeriesID     int64  `bun:"series_id"`
		RelativePath string `bun:"relative_path"`
		SeriesPath   string `bun:"series_path"`
		RootPath     string `bun:"root_path"`
	}
	err := s.db.NewSelect().TableExpr("chapter_files AS f").
		ColumnExpr("f.chapter_id, f.series_id, f.relative_path, s.path AS series_path, r.path AS root_path").
		Join("JOIN series AS s ON s.id = f.series_id").
		Join("JOIN root_folders AS r ON r.id = s.root_folder_id").Scan(ctx, &rows)
	if err != nil {
		return nil, nil, err
	}
	idx := make(map[string]fileRef, len(rows))
	for _, r := range rows {
		idx[filepath.Clean(filepath.Join(r.RootPath, r.SeriesPath, r.RelativePath))] = fileRef{r.ChapterID, r.SeriesID}
	}
	var roots []string
	if err := s.db.NewSelect().Model((*model.RootFolder)(nil)).Column("path").Scan(ctx, &roots); err != nil {
		return nil, nil, err
	}
	return idx, roots, nil
}

// Sync refreshes read states for every reader account.
func (s *Syncer) Sync(ctx context.Context) (Result, error) {
	var res Result
	idx, roots, err := s.index(ctx)
	if err != nil {
		return res, err
	}
	var accounts []model.ReaderAccount
	if err := s.db.NewSelect().Model(&accounts).Scan(ctx); err != nil {
		return res, err
	}
	var errs []error
	for i := range accounts {
		acc := &accounts[i]
		res.Accounts++
		n, err := s.syncAccount(ctx, acc, idx, roots)
		now := time.Now().UTC()
		acc.LastSyncAt = &now
		acc.LastError = ""
		if err != nil {
			res.Failed++
			acc.LastError = err.Error()
			errs = append(errs, err)
			s.log.Warn("read progress sync failed", "reader", acc.ReaderID, "module", acc.ModuleID, "err", err)
		}
		res.Updated += n
		_, _ = s.db.NewUpdate().Model(acc).Column("last_sync_at", "last_error").WherePK().Exec(ctx)
	}
	s.bus.Changed("readers", "sync", 0)
	if res.Accounts > 0 && res.Failed == res.Accounts {
		return res, errors.Join(errs...)
	}
	return res, nil
}

func (s *Syncer) syncAccount(ctx context.Context, acc *model.ReaderAccount, idx map[string]fileRef, roots []string) (int, error) {
	pr, _, err := modules.GetAs[library.ProgressReader](s.mods, acc.ModuleID)
	if err != nil {
		return 0, err
	}
	sctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	progress, err := pr.ReadProgress(sctx, library.Account{Credentials: acc.Credentials}, roots)
	if err != nil {
		return 0, err
	}
	var existing []model.ChapterReadState
	if err := s.db.NewSelect().Model(&existing).Where("reader_id = ?", acc.ReaderID).Scan(ctx); err != nil {
		return 0, err
	}
	byChapter := map[int64]*model.ChapterReadState{}
	for i := range existing {
		byChapter[existing[i].ChapterID] = &existing[i]
	}
	withFile := make(map[int64]bool, len(idx))
	for _, r := range idx {
		withFile[r.chapterID] = true
	}
	now := time.Now().UTC()
	seen := map[int64]bool{}
	updated := 0
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		for _, bp := range progress {
			ref, ok := idx[filepath.Clean(bp.LocalPath)]
			if !ok {
				continue
			}
			seen[ref.chapterID] = true
			st := byChapter[ref.chapterID]
			readAt := bp.ReadAt
			if st == nil {
				if readAt == nil && bp.Completed {
					readAt = &now
				}
				st = &model.ChapterReadState{ReaderID: acc.ReaderID, ChapterID: ref.chapterID, SeriesID: ref.seriesID,
					Completed: bp.Completed, Page: bp.Page, ReadAt: readAt, SyncedAt: now}
				if _, err := tx.NewInsert().Model(st).Exec(ctx); err != nil {
					return err
				}
				updated++
				continue
			}
			if st.Completed == bp.Completed && st.Page == bp.Page && (readAt == nil || (st.ReadAt != nil && st.ReadAt.Equal(*readAt))) {
				continue
			}
			if readAt == nil && bp.Completed && !st.Completed {
				readAt = &now
			}
			if readAt == nil {
				readAt = st.ReadAt
			}
			st.Completed, st.Page, st.ReadAt, st.SyncedAt = bp.Completed, bp.Page, readAt, now
			if _, err := tx.NewUpdate().Model(st).Column("completed", "page", "read_at", "synced_at").WherePK().Exec(ctx); err != nil {
				return err
			}
			updated++
		}
		// Chapters that still have files but are no longer reported were
		// marked unread on the server.
		for chID, st := range byChapter {
			if seen[chID] {
				continue
			}
			if !withFile[chID] {
				continue // cleaned/deleted files are not reported; keep their state
			}
			if _, err := tx.NewDelete().Model(st).WherePK().Exec(ctx); err != nil {
				return err
			}
			updated++
		}
		return nil
	})
	return updated, err
}

// TestAccount validates credentials for a module and returns the server username.
func (s *Syncer) TestAccount(ctx context.Context, moduleID int64, creds map[string]string) (string, error) {
	pr, def, err := modules.GetAs[library.ProgressReader](s.mods, moduleID)
	if err != nil {
		return "", err
	}
	user, err := pr.TestAccount(ctx, library.Account{Credentials: creds})
	if err != nil {
		return "", fmt.Errorf("%s: %w", def.Name, err)
	}
	return user, nil
}
