// Package series manages series: adding (metadata + source links + add
// options), editing, source links, metadata refresh and deletion.
package series

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/events"
	"github.com/Asion001/mangarr/internal/jobs"
	"github.com/Asion001/mangarr/internal/library"
	"github.com/Asion001/mangarr/internal/metadataagg"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/naming"
)

var (
	ErrNotFound = errors.New("series not found")
	ErrExists   = errors.New("series already exists")
)

type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

type Service struct {
	db    *db.DB
	bus   *events.Bus
	lib   *library.Library
	agg   *metadataagg.Aggregator
	mods  *modules.Manager
	queue *jobs.Queue
	log   *slog.Logger
}

func New(d *db.DB, bus *events.Bus, lib *library.Library, agg *metadataagg.Aggregator, mods *modules.Manager, q *jobs.Queue, log *slog.Logger) *Service {
	return &Service{db: d, bus: bus, lib: lib, agg: agg, mods: mods, queue: q, log: log}
}

type SourceLink struct {
	ModuleID   int64  `json:"moduleId"`
	SourceID   string `json:"sourceId"`
	URL        string `json:"url"`
	EngineRef  string `json:"engineRef,omitempty"`
	Title      string `json:"title,omitempty"`
	SourceName string `json:"sourceName,omitempty"`
	Lang       string `json:"lang,omitempty"`
}

type AddRequest struct {
	Metadata         *metadataagg.Ref `json:"metadata,omitempty"`
	Title            string           `json:"title,omitempty"`
	Sources          []SourceLink     `json:"sources"`
	RootFolderID     int64            `json:"rootFolderId"`
	ProfileID        int64            `json:"profileId,omitempty"`
	Monitor          string           `json:"monitor" enum:"all,future,latest,from,none"`
	LatestCount      int              `json:"latestCount,omitempty"`
	FromChapter      float64          `json:"fromChapter,omitempty"`
	MonitorNew       string           `json:"monitorNew,omitempty" enum:"all,none"`
	SearchMissing    bool             `json:"searchMissing"`
	Tags             []int64          `json:"tags,omitempty"`
	ReadingDirection string           `json:"readingDirection,omitempty" enum:"rtl,ltr,vertical,webtoon"`
	Language         string           `json:"language,omitempty"`
	// BlockedScanlators are scanlator names never downloaded for this series.
	BlockedScanlators []string `json:"blockedScanlators,omitempty"`
	// NoRefresh skips queueing the first refresh (the caller syncs itself).
	NoRefresh bool `json:"-"`
}

// Add creates a series, links its sources and queues the first refresh.
func (s *Service) Add(ctx context.Context, req AddRequest) (*model.Series, error) {
	if len(req.Sources) == 0 {
		return nil, ValidationError{"at least one source is required"}
	}
	rf, err := s.lib.RootFolder(ctx, req.RootFolderID)
	if err != nil {
		return nil, ValidationError{"root folder not found"}
	}
	profileID, err := s.profileID(ctx, req.ProfileID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	ser := &model.Series{
		Status: model.StatusUnknown, Monitored: true, MonitorNew: orDefault(req.MonitorNew, model.MonitorAll),
		RootFolderID: rf.ID, ProfileID: profileID, ReadingDirection: orDefault(req.ReadingDirection, "rtl"),
		Tags: req.Tags, AddedAt: now, UpdatedAt: now,
		AddOptions: model.AddOptions{Pending: true, Monitor: orDefault(req.Monitor, model.MonitorAll), LatestCount: req.LatestCount,
			FromChapter: req.FromChapter, SearchMissing: req.SearchMissing},
	}
	if ser.Tags == nil {
		ser.Tags = []int64{}
	}
	ser.BlockedScanlators = cleanNames(req.BlockedScanlators)
	if req.Metadata != nil {
		resolved, err := s.agg.Resolve(ctx, *req.Metadata, nil)
		if err != nil {
			return nil, fmt.Errorf("metadata: %w", err)
		}
		if dup, err := s.findByExternal(ctx, resolved.Metadata.ExternalIDs); err != nil {
			return nil, err
		} else if dup != nil {
			return nil, fmt.Errorf("%w: %s", ErrExists, dup.Title)
		}
		metadataagg.Apply(ser, resolved)
		if req.ReadingDirection != "" {
			ser.ReadingDirection = req.ReadingDirection
		}
	}
	if ser.Title == "" {
		ser.Title = strings.TrimSpace(req.Title)
	}
	if ser.Title == "" {
		ser.Title = strings.TrimSpace(req.Sources[0].Title)
	}
	if ser.Title == "" {
		return nil, ValidationError{"title is required when no metadata is selected"}
	}
	ser.SortTitle = naming.SortTitle(ser.Title)
	ser.Language = firstNonEmpty(req.Language, rf.Language, req.Sources[0].Lang)
	folder, err := s.lib.UniqueFolder(ctx, rf.ID, s.lib.FolderName(ctx, ser.Title, ser.Metadata.Year))
	if err != nil {
		return nil, err
	}
	ser.Path = folder

	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(ser).Exec(ctx); err != nil {
			return err
		}
		for i, l := range req.Sources {
			if _, err := s.insertLink(ctx, tx, ser, l, i); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.lib.EnsureSeriesDir(ctx, ser); err != nil {
		s.log.Warn("create series folder", "series", ser.Title, "err", err)
	}
	if !req.NoRefresh {
		if _, err := s.queue.Push(ctx, "RefreshSeries", map[string]any{"seriesId": ser.ID}, "series-add"); err != nil {
			s.log.Warn("queue refresh", "err", err)
		}
	}
	s.bus.Publish(events.Event{Type: events.SeriesAdded, SeriesID: ser.ID, Payload: events.MessagePayload{Title: "Series added", Message: ser.Title}})
	s.bus.Changed("series", "created", ser.ID)
	return ser, nil
}

func (s *Service) insertLink(ctx context.Context, tx bun.IDB, ser *model.Series, l SourceLink, priority int) (*model.SeriesSource, error) {
	if l.ModuleID == 0 || l.SourceID == "" || l.URL == "" {
		return nil, ValidationError{"source links need moduleId, sourceId and url"}
	}
	if l.SourceName == "" || l.Lang == "" {
		if info := s.lookupSource(ctx, l.ModuleID, l.SourceID); info != nil {
			l.SourceName = firstNonEmpty(l.SourceName, info.DisplayName, info.Name)
			l.Lang = firstNonEmpty(l.Lang, info.Lang)
		}
	}
	ss := &model.SeriesSource{SeriesID: ser.ID, ModuleID: l.ModuleID, SourceID: l.SourceID, SourceName: l.SourceName, Lang: l.Lang,
		MangaURL: l.URL, Title: l.Title, EngineRef: l.EngineRef, Priority: priority, Enabled: true,
		NextCheckAt: time.Now().UTC(), CreatedAt: time.Now().UTC()}
	if _, err := tx.NewInsert().Model(ss).Exec(ctx); err != nil {
		return nil, fmt.Errorf("link source: %w", err)
	}
	return ss, nil
}

func (s *Service) lookupSource(ctx context.Context, moduleID int64, sourceID string) *source.SourceInfo {
	mod, _, err := modules.GetAs[source.Module](s.mods, moduleID)
	if err != nil {
		return nil
	}
	list, err := mod.Sources(ctx)
	if err != nil {
		return nil
	}
	for _, si := range list {
		if si.ID == sourceID {
			return &si
		}
	}
	return nil
}

func (s *Service) profileID(ctx context.Context, id int64) (int64, error) {
	var p model.Profile
	q := s.db.NewSelect().Model(&p)
	if id > 0 {
		q = q.Where("id = ?", id)
	} else {
		q = q.Where("is_default = ?", true)
	}
	if err := q.Limit(1).Scan(ctx); err != nil {
		if id == 0 {
			if err := s.db.NewSelect().Model(&p).Order("id").Limit(1).Scan(ctx); err == nil {
				return p.ID, nil
			}
		}
		return 0, ValidationError{"profile not found"}
	}
	return p.ID, nil
}

func (s *Service) findByExternal(ctx context.Context, ids map[string]string) (*model.Series, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var all []model.Series
	if err := s.db.NewSelect().Model(&all).Column("id", "title", "metadata").Scan(ctx); err != nil {
		return nil, err
	}
	for i := range all {
		for k, v := range ids {
			if k != "mal" && v != "" && all[i].Metadata.ExternalIDs[k] == v {
				return &all[i], nil
			}
		}
	}
	return nil, nil
}

// Get loads a series.
func (s *Service) Get(ctx context.Context, id int64) (*model.Series, error) {
	var ser model.Series
	if err := s.db.NewSelect().Model(&ser).Where("id = ?", id).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &ser, nil
}

type UpdateRequest struct {
	Title            *string   `json:"title,omitempty"`
	Monitored        *bool     `json:"monitored,omitempty"`
	MonitorNew       *string   `json:"monitorNew,omitempty" enum:"all,none"`
	ProfileID        *int64    `json:"profileId,omitempty"`
	Tags             *[]int64  `json:"tags,omitempty"`
	ReadingDirection *string   `json:"readingDirection,omitempty" enum:"rtl,ltr,vertical,webtoon"`
	Language         *string   `json:"language,omitempty"`
	Status           *string   `json:"status,omitempty" enum:"unknown,ongoing,completed,hiatus,cancelled"`
	Description      *string   `json:"description,omitempty"`
	Locks            *[]string `json:"locks,omitempty"`
	// BlockedScanlators replaces the series' blocked scanlator names.
	BlockedScanlators *[]string `json:"blockedScanlators,omitempty"`
	// Location changes are applied by a MoveSeries command (see the API).
	RootFolderID *int64  `json:"rootFolderId,omitempty"`
	Path         *string `json:"path,omitempty"`
	// MoveFiles moves the folder on disk (default true); false only updates
	// the location in mangarr (files moved by hand).
	MoveFiles *bool `json:"moveFiles,omitempty"`
}

// Update edits a series. Editing a metadata field locks it against refreshes.
func (s *Service) Update(ctx context.Context, id int64, req UpdateRequest) (*model.Series, error) {
	ser, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	lock := func(f string) {
		if !ser.Metadata.Locked(f) {
			ser.Metadata.Locks = append(ser.Metadata.Locks, f)
		}
		if ser.Metadata.Provenance == nil {
			ser.Metadata.Provenance = map[string]string{}
		}
		ser.Metadata.Provenance[f] = "user"
	}
	if req.Title != nil && strings.TrimSpace(*req.Title) != "" && *req.Title != ser.Title {
		ser.Title = strings.TrimSpace(*req.Title)
		ser.SortTitle = naming.SortTitle(ser.Title)
		lock("title")
	}
	if req.Monitored != nil {
		ser.Monitored = *req.Monitored
	}
	if req.MonitorNew != nil {
		ser.MonitorNew = *req.MonitorNew
	}
	if req.ProfileID != nil {
		if _, err := s.profileID(ctx, *req.ProfileID); err != nil {
			return nil, err
		}
		ser.ProfileID = *req.ProfileID
	}
	if req.Tags != nil {
		ser.Tags = *req.Tags
	}
	if req.ReadingDirection != nil && *req.ReadingDirection != ser.ReadingDirection {
		ser.ReadingDirection = *req.ReadingDirection
		lock("readingDirection")
	}
	if req.Language != nil {
		ser.Language = *req.Language
	}
	if req.Status != nil && *req.Status != ser.Status {
		ser.Status = *req.Status
		lock("status")
	}
	if req.Description != nil && *req.Description != ser.Metadata.Description {
		ser.Metadata.Description = *req.Description
		lock("description")
	}
	if req.Locks != nil {
		ser.Metadata.Locks = *req.Locks
	}
	if req.BlockedScanlators != nil {
		ser.BlockedScanlators = cleanNames(*req.BlockedScanlators)
	}
	ser.UpdatedAt = time.Now().UTC()
	if _, err := s.db.NewUpdate().Model(ser).WherePK().Exec(ctx); err != nil {
		return nil, err
	}
	s.bus.Changed("series", "updated", ser.ID)
	return ser, nil
}

// Delete removes a series; with deleteFiles its folder goes to the recycle bin.
func (s *Service) Delete(ctx context.Context, id int64, deleteFiles bool) error {
	ser, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	dir, _ := s.lib.SeriesDir(ctx, ser)
	if _, err := s.db.NewDelete().Model((*model.Series)(nil)).Where("id = ?", id).Exec(ctx); err != nil {
		return err
	}
	// tables without FK cascade
	_, _ = s.db.NewDelete().Model((*model.History)(nil)).Where("series_id = ?", id).Exec(ctx)
	if deleteFiles && dir != "" {
		if _, err := os.Stat(dir); err == nil {
			if _, err := s.lib.Recycle(ctx, dir, "", false); err != nil {
				s.log.Warn("recycle series folder", "dir", dir, "err", err)
			}
		}
	}
	s.bus.Publish(events.Event{Type: events.SeriesDeleted, SeriesID: id, Payload: events.MessagePayload{Title: "Series deleted", Message: ser.Title}})
	s.bus.Changed("series", "deleted", id)
	return nil
}

// ---- source links --------------------------------------------------------------

func (s *Service) LinkSource(ctx context.Context, seriesID int64, l SourceLink) (*model.SeriesSource, error) {
	ser, err := s.Get(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	n, err := s.db.NewSelect().Model((*model.SeriesSource)(nil)).Where("series_id = ?", seriesID).Count(ctx)
	if err != nil {
		return nil, err
	}
	ss, err := s.insertLink(ctx, s.db, ser, l, n)
	if err != nil {
		return nil, err
	}
	_, _ = s.queue.Push(ctx, "RefreshSeries", map[string]any{"seriesId": seriesID}, "source-linked")
	s.bus.Changed("series", "updated", seriesID)
	return ss, nil
}

type SourceUpdate struct {
	Priority             *int   `json:"priority,omitempty"`
	Enabled              *bool  `json:"enabled,omitempty"`
	CheckIntervalMinutes *int   `json:"checkIntervalMinutes,omitempty"`
	ModuleID             *int64 `json:"moduleId,omitempty"` // reassign to another compatible module instance
}

func (s *Service) UpdateSource(ctx context.Context, seriesID, linkID int64, u SourceUpdate) (*model.SeriesSource, error) {
	var ss model.SeriesSource
	if err := s.db.NewSelect().Model(&ss).Where("id = ? AND series_id = ?", linkID, seriesID).Scan(ctx); err != nil {
		return nil, ErrNotFound
	}
	if u.Priority != nil {
		ss.Priority = *u.Priority
	}
	if u.Enabled != nil {
		ss.Enabled = *u.Enabled
	}
	if u.CheckIntervalMinutes != nil {
		ss.CheckIntervalMinutes = *u.CheckIntervalMinutes
	}
	if u.ModuleID != nil && *u.ModuleID != ss.ModuleID {
		if _, _, err := modules.GetAs[source.Module](s.mods, *u.ModuleID); err != nil {
			return nil, ValidationError{err.Error()}
		}
		ss.ModuleID = *u.ModuleID
		ss.EngineRef = "" // engine ids are not portable; (sourceId,url) is
		_, _ = s.db.NewUpdate().Model((*model.ChapterRelease)(nil)).Set("engine_ref = ''").Where("series_source_id = ?", ss.ID).Exec(ctx)
	}
	// reset backoff when the user touches a link
	ss.BackoffUntil, ss.ConsecutiveFailures = nil, 0
	ss.NextCheckAt = time.Now().UTC()
	if _, err := s.db.NewUpdate().Model(&ss).WherePK().Exec(ctx); err != nil {
		return nil, err
	}
	s.bus.Changed("series", "updated", seriesID)
	return &ss, nil
}

func (s *Service) UnlinkSource(ctx context.Context, seriesID, linkID int64) error {
	res, err := s.db.NewDelete().Model((*model.SeriesSource)(nil)).Where("id = ? AND series_id = ?", linkID, seriesID).Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.bus.Changed("series", "updated", seriesID)
	return nil
}

// ---- metadata ---------------------------------------------------------------------

// RefreshMetadata re-fetches linked metadata and applies unlocked fields.
func (s *Service) RefreshMetadata(ctx context.Context, id int64) (bool, error) {
	ser, err := s.Get(ctx, id)
	if err != nil {
		return false, err
	}
	var refs []metadataagg.Ref
	for k, v := range ser.Metadata.ExternalIDs {
		refs = append(refs, metadataagg.Ref{Provider: k, ID: v})
	}
	if len(refs) == 0 {
		return false, nil
	}
	resolved, err := s.agg.ResolveRefs(ctx, refs, nil)
	if err != nil {
		return false, err
	}
	oldCover := ser.Metadata.CoverURL
	changed := metadataagg.Apply(ser, resolved)
	now := time.Now().UTC()
	ser.LastMetadataRefresh = &now
	if changed {
		ser.SortTitle = naming.SortTitle(ser.Title)
		ser.UpdatedAt = now
	}
	if _, err := s.db.NewUpdate().Model(ser).WherePK().Exec(ctx); err != nil {
		return false, err
	}
	if changed {
		if oldCover != ser.Metadata.CoverURL {
			_ = s.lib.RefreshCover(ctx, ser, nil)
		}
		s.bus.Changed("series", "updated", ser.ID)
	}
	return changed, nil
}

// LinkMetadata replaces the metadata match of a series (fix a wrong match).
func (s *Service) LinkMetadata(ctx context.Context, id int64, ref metadataagg.Ref) (*model.Series, error) {
	ser, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	resolved, err := s.agg.Resolve(ctx, ref, nil)
	if err != nil {
		return nil, err
	}
	ser.Metadata.ExternalIDs = map[string]string{}
	metadataagg.Apply(ser, resolved)
	ser.SortTitle = naming.SortTitle(ser.Title)
	ser.UpdatedAt = time.Now().UTC()
	if _, err := s.db.NewUpdate().Model(ser).WherePK().Exec(ctx); err != nil {
		return nil, err
	}
	_ = s.lib.RefreshCover(ctx, ser, nil)
	s.bus.Changed("series", "updated", ser.ID)
	return ser, nil
}

// cleanNames trims names and drops empty and duplicate ones.
func cleanNames(names []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[strings.ToLower(n)] {
			continue
		}
		seen[strings.ToLower(n)] = true
		out = append(out, n)
	}
	return out
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}
