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
}

func (s *Server) registerUpdates() {
	tags := []string{"Updates"}
	huma.Register(s.api, huma.Operation{OperationID: "updates-list", Method: http.MethodGet, Path: "/api/v1/updates", Tags: tags,
		Summary: "Recently discovered chapters and newly added titles visible to you"},
		func(ctx context.Context, in *struct {
			Days  int `query:"days" default:"30" minimum:"1" maximum:"365"`
			Limit int `query:"limit" default:"100" minimum:"1" maximum:"500"`
		}) (*struct{ Body []UpdateItem }, error) {
			if in.Days <= 0 || in.Days > 365 {
				in.Days = 30
			}
			if in.Limit <= 0 || in.Limit > 500 {
				in.Limit = 100
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
			items := make([]UpdateItem, 0, in.Limit)
			for key, editions := range groups {
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
				OrderExpr("first_seen_at DESC, id DESC").Limit(in.Limit * 2).Scan(ctx); err != nil {
				return nil, toHTTPError(err)
			}
			chapterIDs := make([]int64, 0, len(chapters))
			for _, chapter := range chapters {
				if _, ok := visible[chapter.SeriesID]; ok {
					chapterIDs = append(chapterIDs, chapter.ID)
				}
			}
			available := map[int64]bool{}
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
			}
			for _, chapter := range chapters {
				series, ok := visible[chapter.SeriesID]
				if !ok {
					continue
				}
				items = append(items, UpdateItem{Kind: "chapter", At: chapter.FirstSeenAt, SeriesID: series.ID, SeriesTitle: series.Title,
					Language: series.Language, CoverURL: seriesCoverURL(series), ChapterID: chapter.ID, Number: chapter.NumberKey,
					Title: chapter.Title, Readable: chapter.FileID != nil || available[chapter.ID]})
			}
			sort.SliceStable(items, func(i, j int) bool { return items[i].At.After(items[j].At) })
			if len(items) > in.Limit {
				items = items[:in.Limit]
			}
			if items == nil {
				items = []UpdateItem{}
			}
			return &struct{ Body []UpdateItem }{items}, nil
		})
}

func seriesCoverURL(series model.Series) string {
	return "api/v1/series/" + strconv.FormatInt(series.ID, 10) + "/cover?v=" + strconv.FormatInt(series.UpdatedAt.Unix(), 10)
}
