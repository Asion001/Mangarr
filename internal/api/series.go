package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"

	"github.com/Asion001/mangarr/internal/decision"
	"github.com/Asion001/mangarr/internal/metadataagg"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/library"
	"github.com/Asion001/mangarr/internal/modules/metadata"
	"github.com/Asion001/mangarr/internal/naming"
	"github.com/Asion001/mangarr/internal/organize"
	"github.com/Asion001/mangarr/internal/series"
)

func init() { register((*Server).registerSeries) }

type SeriesStats struct {
	ChapterCount   int   `json:"chapterCount"`
	MonitoredCount int   `json:"monitoredCount"`
	FileCount      int   `json:"fileCount"`
	MissingCount   int   `json:"missingCount"`
	CleanedCount   int   `json:"cleanedCount"`
	SizeOnDisk     int64 `json:"sizeOnDisk"`
	// SpaceSaved is how much smaller processing (re-encoding) made the files.
	SpaceSaved  int64   `json:"spaceSaved"`
	LastChapter float64 `json:"lastChapter"`
	// Read progress over readers who count for cleanup (all readers when
	// none do): chapters finished, chapters started, last read.
	ReadCount       int        `json:"readCount"`
	InProgressCount int        `json:"inProgressCount"`
	LastReadAt      *time.Time `json:"lastReadAt,omitempty"`
}

// ReadingInfo is a series' reading progress (series detail).
type ReadingInfo struct {
	// NextUnread is the first chapter after the last one read.
	NextUnread *NextChapter     `json:"nextUnread,omitempty"`
	Readers    []ReaderProgress `json:"readers"`
	// WebURL opens the series on a library server (e.g. Komga).
	WebURL  string `json:"webUrl,omitempty"`
	WebName string `json:"webName,omitempty"`
}

type NextChapter struct {
	ChapterID int64  `json:"chapterId"`
	Number    string `json:"number"`
	Title     string `json:"title,omitempty"`
	Available bool   `json:"available"` // has a file
}

type ReaderProgress struct {
	ReaderID   int64      `json:"readerId"`
	Reader     string     `json:"reader"`
	Read       int        `json:"read"`
	InProgress int        `json:"inProgress"`
	LastReadAt *time.Time `json:"lastReadAt,omitempty"`
}

type SeriesResource struct {
	model.Series
	Stats    SeriesStats          `json:"stats"`
	Sources  []model.SeriesSource `json:"sources,omitempty"`
	CoverURL string               `json:"coverUrl"`
	FullPath string               `json:"fullPath,omitempty"`
	Reading  *ReadingInfo         `json:"reading,omitempty"`
}

type statsRow struct {
	SeriesID       int64   `bun:"series_id"`
	ChapterCount   int     `bun:"chapter_count"`
	MonitoredCount int     `bun:"monitored_count"`
	FileCount      int     `bun:"file_count"`
	MissingCount   int     `bun:"missing_count"`
	CleanedCount   int     `bun:"cleaned_count"`
	LastChapter    float64 `bun:"last_chapter"`
}

func (s *Server) seriesStats(ctx context.Context, seriesID int64) (map[int64]SeriesStats, error) {
	var rows []statsRow
	q := s.app.DB.NewSelect().TableExpr("chapters AS c").
		ColumnExpr("c.series_id").
		ColumnExpr("COUNT(*) AS chapter_count").
		ColumnExpr("SUM(CASE WHEN c.monitored THEN 1 ELSE 0 END) AS monitored_count").
		ColumnExpr("SUM(CASE WHEN c.file_id IS NOT NULL THEN 1 ELSE 0 END) AS file_count").
		ColumnExpr("SUM(CASE WHEN c.monitored AND c.file_id IS NULL AND c.state <> 'cleaned' THEN 1 ELSE 0 END) AS missing_count").
		ColumnExpr("SUM(CASE WHEN c.state = 'cleaned' THEN 1 ELSE 0 END) AS cleaned_count").
		ColumnExpr("MAX(c.number_sort) AS last_chapter").
		GroupExpr("c.series_id")
	if seriesID > 0 {
		q = q.Where("c.series_id = ?", seriesID)
	}
	if err := q.Scan(ctx, &rows); err != nil {
		return nil, err
	}
	out := map[int64]SeriesStats{}
	for _, r := range rows {
		out[r.SeriesID] = SeriesStats{ChapterCount: r.ChapterCount, MonitoredCount: r.MonitoredCount, FileCount: r.FileCount,
			MissingCount: r.MissingCount, CleanedCount: r.CleanedCount, LastChapter: r.LastChapter}
	}
	var sizes []struct {
		SeriesID int64 `bun:"series_id"`
		Size     int64 `bun:"size"`
		Saved    int64 `bun:"saved"`
	}
	sq := s.app.DB.NewSelect().TableExpr("chapter_files").
		ColumnExpr("series_id, SUM(size) AS size, SUM(CASE WHEN size_original > size THEN size_original - size ELSE 0 END) AS saved").GroupExpr("series_id")
	if seriesID > 0 {
		sq = sq.Where("series_id = ?", seriesID)
	}
	if err := sq.Scan(ctx, &sizes); err != nil {
		return nil, err
	}
	for _, sz := range sizes {
		st := out[sz.SeriesID]
		st.SizeOnDisk, st.SpaceSaved = sz.Size, sz.Saved
		out[sz.SeriesID] = st
	}
	readers := s.countedReaders(ctx)
	if len(readers) == 0 {
		return out, nil
	}
	var reads []struct {
		SeriesID   int64        `bun:"series_id"`
		Read       int          `bun:"read_count"`
		InProgress int          `bun:"in_progress"`
		LastRead   bun.NullTime `bun:"last_read"`
	}
	rq := s.app.DB.NewSelect().TableExpr("chapter_read_states AS rs").
		ColumnExpr("rs.series_id").
		ColumnExpr("COUNT(DISTINCT CASE WHEN rs.completed THEN rs.chapter_id END) AS read_count").
		ColumnExpr("COUNT(DISTINCT CASE WHEN NOT rs.completed AND rs.page > 0 THEN rs.chapter_id END) AS in_progress").
		ColumnExpr("MAX(rs.read_at) AS last_read").
		Where("rs.reader_id IN (?)", bun.In(readers)).GroupExpr("rs.series_id")
	if seriesID > 0 {
		rq = rq.Where("rs.series_id = ?", seriesID)
	}
	if err := rq.Scan(ctx, &reads); err != nil {
		return nil, err
	}
	for _, r := range reads {
		st := out[r.SeriesID]
		st.ReadCount, st.InProgressCount = r.Read, r.InProgress
		if !r.LastRead.IsZero() {
			t := r.LastRead.Time
			st.LastReadAt = &t
		}
		out[r.SeriesID] = st
	}
	return out, nil
}

// countedReaders are the readers whose progress counts (cleanup readers,
// or everyone when no reader counts for cleanup).
func (s *Server) countedReaders(ctx context.Context) []int64 {
	var readers []model.Reader
	_ = s.app.DB.NewSelect().Model(&readers).Scan(ctx)
	var counted, all []int64
	for _, r := range readers {
		all = append(all, r.ID)
		if r.CountForCleanup {
			counted = append(counted, r.ID)
		}
	}
	if len(counted) > 0 {
		return counted
	}
	return all
}

// readingInfo describes who read what of a series, and what's next.
func (s *Server) readingInfo(ctx context.Context, ser *model.Series, fullPath string) *ReadingInfo {
	info := &ReadingInfo{Readers: []ReaderProgress{}}
	var rows []struct {
		ReaderID   int64        `bun:"reader_id"`
		Name       string       `bun:"name"`
		Read       int          `bun:"read_count"`
		InProgress int          `bun:"in_progress"`
		LastRead   bun.NullTime `bun:"last_read"`
	}
	_ = s.app.DB.NewSelect().TableExpr("chapter_read_states AS rs").Join("JOIN readers AS r ON r.id = rs.reader_id").
		ColumnExpr("rs.reader_id, r.name").
		ColumnExpr("SUM(CASE WHEN rs.completed THEN 1 ELSE 0 END) AS read_count").
		ColumnExpr("SUM(CASE WHEN NOT rs.completed AND rs.page > 0 THEN 1 ELSE 0 END) AS in_progress").
		ColumnExpr("MAX(rs.read_at) AS last_read").
		Where("rs.series_id = ?", ser.ID).GroupExpr("rs.reader_id, r.name").OrderExpr("r.name").Scan(ctx, &rows)
	for _, r := range rows {
		rp := ReaderProgress{ReaderID: r.ReaderID, Reader: r.Name, Read: r.Read, InProgress: r.InProgress}
		if !r.LastRead.IsZero() {
			t := r.LastRead.Time
			rp.LastReadAt = &t
		}
		info.Readers = append(info.Readers, rp)
	}
	if readers := s.countedReaders(ctx); len(readers) > 0 {
		// the first chapter after the highest one read
		var maxRead float64
		err := s.app.DB.NewSelect().TableExpr("chapter_read_states AS rs").Join("JOIN chapters AS c ON c.id = rs.chapter_id").
			ColumnExpr("COALESCE(MAX(c.number_sort), -1)").Where("rs.series_id = ? AND rs.completed AND rs.reader_id IN (?)", ser.ID, bun.In(readers)).
			Scan(ctx, &maxRead)
		if err == nil && maxRead >= 0 {
			var next model.Chapter
			if err := s.app.DB.NewSelect().Model(&next).Where("series_id = ? AND number_sort > ?", ser.ID, maxRead).
				Order("number_sort").Limit(1).Scan(ctx); err == nil {
				info.NextUnread = &NextChapter{ChapterID: next.ID, Number: next.NumberKey, Title: next.Title, Available: next.FileID != nil}
			}
		}
	}
	if fullPath != "" {
		for _, m := range modules.ActiveAs[library.WebLinker](s.app.Modules, modules.KindLibrary) {
			wctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			u, err := m.Instance.SeriesURL(wctx, fullPath)
			cancel()
			if err == nil && u != "" {
				info.WebURL, info.WebName = u, m.Def.Name
				break
			}
		}
	}
	return info
}

func (s *Server) seriesResource(ctx context.Context, ser model.Series, stats map[int64]SeriesStats, detail bool) SeriesResource {
	r := SeriesResource{Series: ser, Stats: stats[ser.ID],
		CoverURL: "api/v1/series/" + strconv.FormatInt(ser.ID, 10) + "/cover?v=" + strconv.FormatInt(ser.UpdatedAt.Unix(), 10)}
	if detail {
		_ = s.app.DB.NewSelect().Model(&r.Sources).Where("series_id = ?", ser.ID).Order("priority", "id").Scan(ctx)
		r.FullPath, _ = s.app.Library.SeriesDir(ctx, &ser)
		r.Reading = s.readingInfo(ctx, &ser, r.FullPath)
	}
	return r
}

type ReleaseView struct {
	model.ChapterRelease
	SourceName  string `json:"sourceName"`
	Priority    int    `json:"priority"`
	Blocklisted bool   `json:"blocklisted"`
}

type ReadStateView struct {
	ReaderID  int64      `json:"readerId"`
	Reader    string     `json:"reader"`
	Completed bool       `json:"completed"`
	Page      int        `json:"page"`
	ReadAt    *time.Time `json:"readAt,omitempty"`
}

type ChapterResource struct {
	model.Chapter
	File     *model.ChapterFile `json:"file,omitempty"`
	Releases []ReleaseView      `json:"releases"`
	ReadBy   []ReadStateView    `json:"readBy"`
	Job      *model.DownloadJob `json:"job,omitempty"`
}

func (s *Server) chapterResources(ctx context.Context, seriesID int64) ([]ChapterResource, error) {
	db := s.app.DB
	var chapters []model.Chapter
	if err := db.NewSelect().Model(&chapters).Where("series_id = ?", seriesID).Order("number_sort DESC").Scan(ctx); err != nil {
		return nil, err
	}
	var files []model.ChapterFile
	if err := db.NewSelect().Model(&files).Where("series_id = ?", seriesID).Scan(ctx); err != nil {
		return nil, err
	}
	var sources []model.SeriesSource
	if err := db.NewSelect().Model(&sources).Where("series_id = ?", seriesID).Scan(ctx); err != nil {
		return nil, err
	}
	var rels []model.ChapterRelease
	if err := db.NewSelect().Model(&rels).Where("series_id = ?", seriesID).Order("id").Scan(ctx); err != nil {
		return nil, err
	}
	var bl []model.Blocklist
	_ = db.NewSelect().Model(&bl).Where("series_id = ?", seriesID).Scan(ctx)
	var jobs []model.DownloadJob
	_ = db.NewSelect().Model(&jobs).Where("series_id = ?", seriesID).Order("id").Scan(ctx)
	type rs struct {
		model.ChapterReadState
		Name string `bun:"name"`
	}
	var reads []rs
	_ = db.NewSelect().TableExpr("chapter_read_states AS r").ColumnExpr("r.*, rd.name").
		Join("JOIN readers AS rd ON rd.id = r.reader_id").Where("r.series_id = ?", seriesID).Scan(ctx, &reads)

	fileBy := map[int64]*model.ChapterFile{}
	for i := range files {
		fileBy[files[i].ChapterID] = &files[i]
	}
	srcBy := map[int64]model.SeriesSource{}
	for _, ss := range sources {
		srcBy[ss.ID] = ss
	}
	blocked := map[string]bool{}
	for _, b := range bl {
		blocked[strconv.FormatInt(b.SeriesSourceID, 10)+"|"+b.ChapterURL] = true
	}
	relBy := map[int64][]ReleaseView{}
	for _, r := range rels {
		if r.ChapterID == nil {
			continue
		}
		ss := srcBy[r.SeriesSourceID]
		relBy[*r.ChapterID] = append(relBy[*r.ChapterID], ReleaseView{ChapterRelease: r, SourceName: ss.SourceName, Priority: ss.Priority,
			Blocklisted: blocked[strconv.FormatInt(r.SeriesSourceID, 10)+"|"+r.ChapterURL]})
	}
	jobBy := map[int64]*model.DownloadJob{}
	for i := range jobs {
		jobBy[jobs[i].ChapterID] = &jobs[i] // latest wins (ordered by id)
	}
	readBy := map[int64][]ReadStateView{}
	for _, r := range reads {
		readBy[r.ChapterID] = append(readBy[r.ChapterID], ReadStateView{ReaderID: r.ReaderID, Reader: r.Name, Completed: r.Completed, Page: r.Page, ReadAt: r.ReadAt})
	}
	out := make([]ChapterResource, 0, len(chapters))
	for _, ch := range chapters {
		cr := ChapterResource{Chapter: ch, File: fileBy[ch.ID], Releases: relBy[ch.ID], ReadBy: readBy[ch.ID], Job: jobBy[ch.ID]}
		if cr.Releases == nil {
			cr.Releases = []ReleaseView{}
		}
		if cr.ReadBy == nil {
			cr.ReadBy = []ReadStateView{}
		}
		out = append(out, cr)
	}
	return out, nil
}

func seriesError(err error) error {
	var ve series.ValidationError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &ve):
		return huma.Error400BadRequest(ve.Msg)
	case errors.Is(err, series.ErrNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, series.ErrExists):
		return huma.Error409Conflict(err.Error())
	}
	return toHTTPError(err)
}

type LookupResult struct {
	metadataagg.Candidate
	ExistingSeriesID int64 `json:"existingSeriesId,omitempty"`
}

// moveAfterUpdate queues a MoveSeries command when the root folder or folder
// changed, or when the title changed and folders follow titles.
func (s *Server) moveAfterUpdate(ctx context.Context, before, after model.Series, req series.UpdateRequest) error {
	move := organize.MoveRequest{SeriesID: after.ID, MoveFiles: req.MoveFiles == nil || *req.MoveFiles}
	if req.RootFolderID != nil && *req.RootFolderID != before.RootFolderID {
		if _, err := s.app.Library.RootFolder(ctx, *req.RootFolderID); err != nil {
			return huma.Error400BadRequest("unknown root folder")
		}
		move.RootFolderID = *req.RootFolderID
	}
	if req.Path != nil && strings.TrimSpace(*req.Path) != "" && *req.Path != before.Path {
		move.Path = *req.Path
	} else if before.Title != after.Title {
		if mm, _ := s.app.Settings.MediaManagement(ctx); mm.RenameFolderOnTitleChange {
			if name := naming.Sanitize(s.app.Library.FolderName(ctx, after.Title, after.Metadata.Year)); name != "" && name != before.Path {
				move.Path = name
			}
		}
	}
	if move.RootFolderID == 0 && move.Path == "" {
		return nil
	}
	_, err := s.app.Queue.Push(ctx, "MoveSeries", toBody(move), "series-edit")
	return toHTTPError(err)
}

// toBody converts a request struct to a command body.
func toBody(v any) map[string]any {
	b, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

// existingByExternalID returns a matcher from external ids to series in the library.
func (s *Server) existingByExternalID(ctx context.Context) func(ids map[string]string) int64 {
	var existing []model.Series
	_ = s.app.DB.NewSelect().Model(&existing).Column("id", "metadata").Scan(ctx)
	return func(ids map[string]string) int64 {
		for _, e := range existing {
			for k, v := range ids {
				if k != "mal" && v != "" && e.Metadata.ExternalIDs[k] == v {
					return e.ID
				}
			}
		}
		return 0
	}
}

func (s *Server) registerSeries() {
	tags := []string{"Series"}
	huma.Register(s.api, huma.Operation{OperationID: "series-list", Method: http.MethodGet, Path: "/api/v1/series", Tags: tags},
		func(ctx context.Context, _ *struct{}) (*struct{ Body []SeriesResource }, error) {
			var list []model.Series
			if err := s.app.DB.NewSelect().Model(&list).Order("sort_title").Scan(ctx); err != nil {
				return nil, toHTTPError(err)
			}
			stats, err := s.seriesStats(ctx, 0)
			if err != nil {
				return nil, toHTTPError(err)
			}
			out := make([]SeriesResource, 0, len(list))
			for _, ser := range list {
				out = append(out, s.seriesResource(ctx, ser, stats, false))
			}
			return &struct{ Body []SeriesResource }{out}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "series-get", Method: http.MethodGet, Path: "/api/v1/series/{id}", Tags: tags},
		func(ctx context.Context, in *IDPath) (*struct{ Body SeriesResource }, error) {
			ser, err := s.app.Series.Get(ctx, in.ID)
			if err != nil {
				return nil, seriesError(err)
			}
			stats, err := s.seriesStats(ctx, in.ID)
			if err != nil {
				return nil, toHTTPError(err)
			}
			return &struct{ Body SeriesResource }{s.seriesResource(ctx, *ser, stats, true)}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "series-lookup", Method: http.MethodGet, Path: "/api/v1/series/lookup", Tags: tags,
		Summary: "Search metadata modules (merged by priority) for a new series"},
		func(ctx context.Context, in *struct {
			Query string `query:"q" minLength:"1"`
		}) (*struct {
			Body struct {
				Results []LookupResult `json:"results"`
				Errors  []string       `json:"errors"`
			}
		}, error) {
			cands, errs := s.app.Metadata.Search(ctx, in.Query, 10)
			out := &struct {
				Body struct {
					Results []LookupResult `json:"results"`
					Errors  []string       `json:"errors"`
				}
			}{}
			out.Body.Results, out.Body.Errors = []LookupResult{}, []string{}
			existing := s.existingByExternalID(ctx)
			for _, c := range cands {
				out.Body.Results = append(out.Body.Results, LookupResult{Candidate: c, ExistingSeriesID: existing(c.ExternalIDs)})
			}
			for _, e := range errs {
				out.Body.Errors = append(out.Body.Errors, e.Error())
			}
			return out, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "series-lookup-get", Method: http.MethodGet, Path: "/api/v1/series/lookup/{moduleId}/{id}", Tags: tags,
		Summary: "Get one metadata result by module and provider id"},
		func(ctx context.Context, in *struct {
			ModuleID int64  `path:"moduleId"`
			ID       string `path:"id"`
		}) (*struct{ Body LookupResult }, error) {
			mod, def, err := modules.GetAs[metadata.Module](s.app.Modules, in.ModuleID)
			if err != nil {
				return nil, huma.Error404NotFound(err.Error())
			}
			md, err := mod.Get(ctx, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound(err.Error())
			}
			c := metadataagg.Candidate{SeriesMetadata: *md, ModuleID: def.ID, ModuleName: def.Name}
			return &struct{ Body LookupResult }{LookupResult{Candidate: c, ExistingSeriesID: s.existingByExternalID(ctx)(md.ExternalIDs)}}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "series-add", Method: http.MethodPost, Path: "/api/v1/series", Tags: tags},
		func(ctx context.Context, in *struct{ Body series.AddRequest }) (*struct{ Body SeriesResource }, error) {
			ser, err := s.app.Series.Add(ctx, in.Body)
			if err != nil {
				return nil, seriesError(err)
			}
			return &struct{ Body SeriesResource }{s.seriesResource(ctx, *ser, nil, true)}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "series-update", Method: http.MethodPut, Path: "/api/v1/series/{id}", Tags: tags},
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body series.UpdateRequest
		}) (*struct{ Body SeriesResource }, error) {
			var before model.Series
			if err := s.app.DB.NewSelect().Model(&before).Where("id = ?", in.ID).Scan(ctx); err != nil {
				return nil, huma.Error404NotFound("series not found")
			}
			ser, err := s.app.Series.Update(ctx, in.ID, in.Body)
			if err != nil {
				return nil, seriesError(err)
			}
			if in.Body.ProfileID != nil {
				s.app.PushProcessBacklog("series-profile") // the new profile may process differently
			}
			if err := s.moveAfterUpdate(ctx, before, *ser, in.Body); err != nil {
				return nil, err
			}
			stats, _ := s.seriesStats(ctx, in.ID)
			return &struct{ Body SeriesResource }{s.seriesResource(ctx, *ser, stats, true)}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "series-delete", Method: http.MethodDelete, Path: "/api/v1/series/{id}", Tags: tags},
		func(ctx context.Context, in *struct {
			ID          int64 `path:"id"`
			DeleteFiles bool  `query:"deleteFiles"`
		}) (*struct{}, error) {
			return nil, seriesError(s.app.Series.Delete(ctx, in.ID, in.DeleteFiles))
		})

	huma.Register(s.api, huma.Operation{OperationID: "series-chapters", Method: http.MethodGet, Path: "/api/v1/series/{id}/chapters", Tags: tags},
		func(ctx context.Context, in *IDPath) (*struct{ Body []ChapterResource }, error) {
			out, err := s.chapterResources(ctx, in.ID)
			return &struct{ Body []ChapterResource }{out}, toHTTPError(err)
		})

	huma.Register(s.api, huma.Operation{OperationID: "chapters-monitor", Method: http.MethodPut, Path: "/api/v1/chapters/monitor", Tags: tags},
		func(ctx context.Context, in *struct {
			Body struct {
				ChapterIDs []int64 `json:"chapterIds" minItems:"1"`
				Monitored  bool    `json:"monitored"`
			}
		}) (*struct{}, error) {
			_, err := s.app.DB.NewUpdate().Model((*model.Chapter)(nil)).Set("monitored = ?", in.Body.Monitored).
				Set("updated_at = ?", time.Now().UTC()).Where("id IN (?)", bun.In(in.Body.ChapterIDs)).Exec(ctx)
			s.app.Bus.Changed("chapter", "updated", 0)
			return nil, toHTTPError(err)
		})

	huma.Register(s.api, huma.Operation{OperationID: "chapter-decision", Method: http.MethodGet, Path: "/api/v1/series/{id}/chapters/{chapterId}/decision", Tags: tags,
		Summary: "Explain why a chapter would or would not be downloaded"},
		func(ctx context.Context, in *struct {
			ID        int64 `path:"id"`
			ChapterID int64 `path:"chapterId"`
		}) (*struct {
			Body struct {
				Decision *decision.Decision `json:"decision"`
				Approved *ReleaseView       `json:"approved,omitempty"`
			}
		}, error) {
			d, best, err := s.app.Searcher.Explain(ctx, in.ID, in.ChapterID)
			if err != nil {
				return nil, toHTTPError(err)
			}
			out := &struct {
				Body struct {
					Decision *decision.Decision `json:"decision"`
					Approved *ReleaseView       `json:"approved,omitempty"`
				}
			}{}
			if d == nil {
				d = &decision.Decision{}
			}
			if d.Rejections == nil {
				d.Rejections = []decision.Rejection{}
			}
			out.Body.Decision = d
			if best != nil {
				out.Body.Approved = &ReleaseView{ChapterRelease: best.Release, SourceName: best.Source.SourceName, Priority: best.Source.Priority}
			}
			return out, nil
		})

	type commandResult struct{ Body *model.Command }
	push := func(ctx context.Context, name string, body map[string]any) (*commandResult, error) {
		c, err := s.app.Queue.Push(ctx, name, body, "manual")
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return &commandResult{c}, nil
	}
	huma.Register(s.api, huma.Operation{OperationID: "series-refresh", Method: http.MethodPost, Path: "/api/v1/series/{id}/refresh", Tags: tags},
		func(ctx context.Context, in *IDPath) (*commandResult, error) {
			return push(ctx, "RefreshSeries", map[string]any{"seriesId": in.ID})
		})
	huma.Register(s.api, huma.Operation{OperationID: "series-search", Method: http.MethodPost, Path: "/api/v1/series/{id}/search", Tags: tags,
		Summary: "Grab missing chapters. With chapterIds, only those (explicit search ignores monitoring)."},
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body *struct {
				ChapterIDs []int64 `json:"chapterIds,omitempty"`
			}
		}) (*commandResult, error) {
			body := map[string]any{"seriesId": in.ID}
			if in.Body != nil && len(in.Body.ChapterIDs) > 0 {
				body["chapterIds"], body["explicit"] = in.Body.ChapterIDs, true
			}
			return push(ctx, "SearchMissing", body)
		})
	huma.Register(s.api, huma.Operation{OperationID: "series-metadata-refresh", Method: http.MethodPost, Path: "/api/v1/series/{id}/metadata/refresh", Tags: tags},
		func(ctx context.Context, in *IDPath) (*commandResult, error) {
			return push(ctx, "RefreshMetadata", map[string]any{"seriesId": in.ID})
		})
	huma.Register(s.api, huma.Operation{OperationID: "series-metadata-link", Method: http.MethodPut, Path: "/api/v1/series/{id}/metadata", Tags: tags,
		Summary: "Replace the metadata match of a series"},
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body metadataagg.Ref
		}) (*struct{ Body SeriesResource }, error) {
			ser, err := s.app.Series.LinkMetadata(ctx, in.ID, in.Body)
			if err != nil {
				return nil, seriesError(err)
			}
			return &struct{ Body SeriesResource }{s.seriesResource(ctx, *ser, nil, true)}, nil
		})

	// ---- source links
	huma.Register(s.api, huma.Operation{OperationID: "series-source-link", Method: http.MethodPost, Path: "/api/v1/series/{id}/sources", Tags: tags},
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body series.SourceLink
		}) (*struct{ Body *model.SeriesSource }, error) {
			ss, err := s.app.Series.LinkSource(ctx, in.ID, in.Body)
			if err != nil {
				if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "duplicate") {
					return nil, huma.Error409Conflict("source already linked")
				}
				return nil, seriesError(err)
			}
			return &struct{ Body *model.SeriesSource }{ss}, nil
		})
	huma.Register(s.api, huma.Operation{OperationID: "series-source-update", Method: http.MethodPut, Path: "/api/v1/series/{id}/sources/{linkId}", Tags: tags},
		func(ctx context.Context, in *struct {
			ID     int64 `path:"id"`
			LinkID int64 `path:"linkId"`
			Body   series.SourceUpdate
		}) (*struct{ Body *model.SeriesSource }, error) {
			ss, err := s.app.Series.UpdateSource(ctx, in.ID, in.LinkID, in.Body)
			return &struct{ Body *model.SeriesSource }{ss}, seriesError(err)
		})
	huma.Register(s.api, huma.Operation{OperationID: "series-source-unlink", Method: http.MethodDelete, Path: "/api/v1/series/{id}/sources/{linkId}", Tags: tags},
		func(ctx context.Context, in *struct {
			ID     int64 `path:"id"`
			LinkID int64 `path:"linkId"`
		}) (*struct{}, error) {
			return nil, seriesError(s.app.Series.UnlinkSource(ctx, in.ID, in.LinkID))
		})

	huma.Register(s.api, huma.Operation{OperationID: "series-cover", Method: http.MethodGet, Path: "/api/v1/series/{id}/cover", Tags: tags},
		func(ctx context.Context, in *struct {
			ID   int64  `path:"id"`
			V    string `query:"v"`
			Size string `query:"size" enum:",full" doc:"full = the library's cover.jpg as is (default: a resized copy)"`
		}) (*imageOutput, error) {
			ser, err := s.app.Series.Get(ctx, in.ID)
			if err != nil {
				return nil, seriesError(err)
			}
			if in.Size == "full" {
				if p := s.app.Library.CoverPath(ctx, ser); p != "" {
					if data, err := os.ReadFile(p); err == nil {
						return &imageOutput{ContentType: http.DetectContentType(data), CacheControl: "public, max-age=3600", Body: data}, nil
					}
				}
			}
			data, ct, err := s.app.Reading.Cover(ctx, ser)
			if err != nil {
				return nil, huma.Error404NotFound("no cover")
			}
			return &imageOutput{ContentType: ct, CacheControl: "public, max-age=3600", Body: data}, nil
		})
}
