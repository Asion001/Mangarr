package api

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/Asion001/mangarr/internal/model"
	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"
)

func init() { register((*Server).registerReadPicker) }

type ReadChapterItem struct {
	ID         int64   `json:"id"`
	Number     string  `json:"number"`
	NumberSort float64 `json:"numberSort"`
	Volume     string  `json:"volume,omitempty"`
	Title      string  `json:"title,omitempty"`
	Available  bool    `json:"available"`
	Downloaded bool    `json:"downloaded"`
	Completed  bool    `json:"completed"`
	Page       int     `json:"page,omitempty"`
}

type ReadChapterPickerResponse struct {
	Items    []ReadChapterItem `json:"items"`
	Page     int               `json:"page"`
	PageSize int               `json:"pageSize"`
	Total    int               `json:"total"`
}

func (s *Server) registerReadPicker() {
	huma.Register(s.api, huma.Operation{OperationID: "read-chapter-picker", Method: http.MethodGet,
		Path: "/api/v1/read/series/{id}/chapters", Tags: []string{"Reading"}, Summary: "Lightweight chapter list for the web reader picker"},
		func(ctx context.Context, in *struct {
			ID        int64  `path:"id"`
			Query     string `query:"q" maxLength:"100"`
			CurrentID int64  `query:"currentId,omitempty"`
			Page      int    `query:"page" minimum:"0" default:"0"`
			PageSize  int    `query:"pageSize" minimum:"20" maximum:"100" default:"50"`
		}) (*struct{ Body ReadChapterPickerResponse }, error) {
			if _, err := s.visibleSeries(ctx, in.ID); err != nil {
				return nil, err
			}
			readerID, err := s.readerOf(ctx)
			if err != nil {
				return nil, readError(err)
			}
			var chapters []model.Chapter
			if err := s.app.DB.NewSelect().Model(&chapters).Where("series_id = ?", in.ID).Order("number_sort", "id").Scan(ctx); err != nil {
				return nil, toHTTPError(err)
			}
			needle := strings.ToLower(strings.TrimSpace(in.Query))
			if needle != "" {
				filtered := chapters[:0]
				for _, chapter := range chapters {
					if strings.Contains(strings.ToLower(chapter.NumberKey), needle) || strings.Contains(strings.ToLower(chapter.Title), needle) || strings.Contains(strings.ToLower(chapter.Volume), needle) {
						filtered = append(filtered, chapter)
					}
				}
				chapters = filtered
			}
			total := len(chapters)
			pageSize := min(max(in.PageSize, 20), 100)
			page := in.Page
			if page == 0 {
				page = 1
				if in.CurrentID > 0 && needle == "" {
					for index, chapter := range chapters {
						if chapter.ID == in.CurrentID {
							page = index/pageSize + 1
							break
						}
					}
				}
			}
			start := min((page-1)*pageSize, total)
			end := min(start+pageSize, total)
			pageChapters := chapters[start:end]
			ids := make([]int64, len(pageChapters))
			for i := range pageChapters {
				ids[i] = pageChapters[i].ID
			}
			releases := map[int64]bool{}
			states := map[int64]model.ChapterReadState{}
			if len(ids) > 0 {
				var releaseRows []struct {
					ChapterID int64 `bun:"chapter_id"`
				}
				if err := s.app.DB.NewSelect().Model((*model.ChapterRelease)(nil)).Column("chapter_id").Where("chapter_id IN (?)", bun.In(ids)).Where("removed = ?", false).Group("chapter_id").Scan(ctx, &releaseRows); err != nil {
					return nil, toHTTPError(err)
				}
				for _, row := range releaseRows {
					releases[row.ChapterID] = true
				}
				var stateRows []model.ChapterReadState
				if err := s.app.DB.NewSelect().Model(&stateRows).Where("reader_id = ? AND chapter_id IN (?)", readerID, bun.In(ids)).Scan(ctx); err != nil {
					return nil, toHTTPError(err)
				}
				for _, state := range stateRows {
					states[state.ChapterID] = state
				}
			}
			items := make([]ReadChapterItem, 0, len(pageChapters))
			for _, chapter := range pageChapters {
				state := states[chapter.ID]
				items = append(items, ReadChapterItem{ID: chapter.ID, Number: chapter.NumberKey, NumberSort: chapter.NumberSort, Volume: chapter.Volume,
					Title: chapter.Title, Downloaded: chapter.FileID != nil, Available: chapter.FileID != nil || releases[chapter.ID], Completed: state.Completed, Page: state.Page})
			}
			sort.SliceStable(items, func(i, j int) bool { return items[i].NumberSort < items[j].NumberSort })
			return &struct{ Body ReadChapterPickerResponse }{ReadChapterPickerResponse{Items: items, Page: page, PageSize: pageSize, Total: total}}, nil
		})
}
