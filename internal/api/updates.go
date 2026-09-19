package api

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"

	"github.com/Asion001/mangarr/internal/access"
	"github.com/Asion001/mangarr/internal/model"
)

func init() { register((*Server).registerUpdates) }

// UpdateItem is a title added to the library or a chapter first discovered.
type UpdateItem struct {
	Kind        string    `json:"kind" enum:"series,chapter"`
	At          time.Time `json:"at"`
	SeriesID    int64     `json:"seriesId"`
	SeriesTitle string    `json:"seriesTitle"`
	Language    string    `json:"language,omitempty"`
	Languages   []string  `json:"languages,omitempty"`
	CoverURL    string    `json:"coverUrl"`
	ChapterID   int64     `json:"chapterId,omitempty"`
	Number      string    `json:"number,omitempty"`
	Title       string    `json:"title,omitempty"`
	Readable    bool      `json:"readable,omitempty"`
	Downloaded  bool      `json:"downloaded,omitempty"`
	ReadState   string    `json:"readState,omitempty" enum:",unread,in_progress,read"`
	ReadPage    int       `json:"readPage,omitempty"`
}

// UpdatePage is a stable page of the reader-facing update feed.
type UpdatePage struct {
	Items    []UpdateItem `json:"items"`
	Total    int          `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"pageSize"`
}

func (s *Server) registerUpdates() {
	tags := []string{"Updates"}
	huma.Register(s.api, huma.Operation{OperationID: "updates-list", Method: http.MethodGet, Path: "/api/v1/updates", Tags: tags,
		Summary: "Recently discovered chapters and newly added titles visible to you; initial catalog chapters are suppressed"},
		func(ctx context.Context, in *struct {
			Days     int    `query:"days" default:"30" minimum:"1" maximum:"365"`
			Kind     string `query:"kind" enum:",all,chapter,series"`
			Page     int    `query:"page" default:"1" minimum:"1"`
			PageSize int    `query:"pageSize" default:"100" minimum:"1" maximum:"500"`
		}) (*struct{ Body UpdatePage }, error) {
			if in.Days <= 0 || in.Days > 365 {
				in.Days = 30
			}
			if in.Page <= 0 {
				in.Page = 1
			}
			if in.PageSize <= 0 || in.PageSize > 500 {
				in.PageSize = 100
			}
			cutoff := time.Now().UTC().Add(-time.Duration(in.Days) * 24 * time.Hour)
			var seriesRows []model.Series
			if err := s.app.DB.NewSelect().Model(&seriesRows).Order("id").Scan(ctx); err != nil {
				return nil, toHTTPError(err)
			}
			viewer := access.From(ctx)
			visible := map[int64]model.Series{}
			for _, series := range seriesRows {
				if viewer == nil || viewer.Sees(&series) {
					visible[series.ID] = series
				}
			}

			var works []model.Work
			_ = s.app.DB.NewSelect().Model(&works).Scan(ctx)
			workByID := map[int64]model.Work{}
			for _, work := range works {
				workByID[work.ID] = work
			}
			groups := map[int64][]model.Series{}
			for _, series := range seriesRows {
				if _, ok := visible[series.ID]; !ok {
					continue
				}
				key := series.WorkID
				if key == 0 {
					key = -series.ID
				}
				groups[key] = append(groups[key], series)
			}
			items := make([]UpdateItem, 0, in.PageSize)
			for key, editions := range groups {
				if in.Kind == "chapter" {
					continue
				}
				representative := editions[0]
				title, at := representative.Title, representative.AddedAt
				if work, ok := workByID[key]; ok {
					title, at = work.Title, work.CreatedAt
				}
				if at.Before(cutoff) {
					continue
				}
				languages := make([]string, 0, len(editions))
				for _, edition := range editions {
					if edition.Language != "" {
						languages = append(languages, edition.Language)
					}
				}
				sort.Strings(languages)
				items = append(items, UpdateItem{Kind: "series", At: at, SeriesID: representative.ID, SeriesTitle: title,
					Languages: languages, CoverURL: seriesCoverURL(representative)})
			}

			var chapters []model.Chapter
			if err := s.app.DB.NewSelect().Model(&chapters).Where("first_seen_at >= ?", cutoff).
				OrderExpr("first_seen_at DESC, id DESC").Scan(ctx); err != nil {
				return nil, toHTTPError(err)
			}
			chapterIDs := make([]int64, 0, len(chapters))
			for _, chapter := range chapters {
				if _, ok := visible[chapter.SeriesID]; ok {
					chapterIDs = append(chapterIDs, chapter.ID)
				}
			}
			available := map[int64]bool{}
			readState := map[int64]model.ChapterReadState{}
			if len(chapterIDs) > 0 {
				type row struct {
					ChapterID int64 `bun:"chapter_id"`
				}
				var rows []row
				_ = s.app.DB.NewSelect().TableExpr("chapter_releases").ColumnExpr("DISTINCT chapter_id").
					Where("chapter_id IN (?) AND removed = ?", bun.In(chapterIDs), false).Scan(ctx, &rows)
				for _, row := range rows {
					available[row.ChapterID] = true
				}
				if viewer != nil && viewer.ReaderID > 0 {
					var states []model.ChapterReadState
					_ = s.app.DB.NewSelect().Model(&states).Where("reader_id = ?", viewer.ReaderID).
						Where("chapter_id IN (?)", bun.In(chapterIDs)).Scan(ctx)
					for _, state := range states {
						readState[state.ChapterID] = state
					}
				}
			}
			for _, chapter := range chapters {
				if in.Kind == "series" {
					continue
				}
				series, ok := visible[chapter.SeriesID]
				if !ok {
					continue
				}
				// A title's first refresh imports its whole historical catalog. Treat
				// chapters found in that five-minute window as part of the new-title
				// event instead of flooding the feed with old chapters.
				if !series.AddedAt.IsZero() && !chapter.FirstSeenAt.After(series.AddedAt.Add(5*time.Minute)) {
					continue
				}
				state := "unread"
				readPage := 0
				if progress, ok := readState[chapter.ID]; ok {
					readPage = progress.Page
					if progress.Completed {
						state = "read"
					} else if progress.Page > 0 {
						state = "in_progress"
					}
				}
				items = append(items, UpdateItem{Kind: "chapter", At: chapter.FirstSeenAt, SeriesID: series.ID, SeriesTitle: series.Title,
					Language: series.Language, CoverURL: seriesCoverURL(series), ChapterID: chapter.ID, Number: chapter.NumberKey,
					Title: chapter.Title, Readable: chapter.FileID != nil || available[chapter.ID], Downloaded: chapter.FileID != nil,
					ReadState: state, ReadPage: readPage})
			}
			sort.SliceStable(items, func(i, j int) bool { return items[i].At.After(items[j].At) })
			total := len(items)
			start := min((in.Page-1)*in.PageSize, total)
			end := min(start+in.PageSize, total)
			page := UpdatePage{Items: items[start:end], Total: total, Page: in.Page, PageSize: in.PageSize}
			if page.Items == nil {
				page.Items = []UpdateItem{}
			}
			return &struct{ Body UpdatePage }{page}, nil
		})
}

func seriesCoverURL(series model.Series) string {
	return "api/v1/series/" + strconv.FormatInt(series.ID, 10) + "/cover?v=" + strconv.FormatInt(series.UpdatedAt.Unix(), 10)
}
