package series

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/uptrace/bun"

	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/sourcesearch"
)

// Bulk actions over many series' sources.
const (
	// BulkAdd finds each series at a catalog and links it as a fallback (last
	// in priority), which is how a backup source is added to a whole library.
	BulkAdd = "add"
	// BulkRemove unlinks a catalog from the selected series.
	BulkRemove = "remove"
	// BulkEnable and BulkDisable keep the link but change whether it is used.
	BulkEnable  = "enable"
	BulkDisable = "disable"
)

// BulkRequest selects series and says what to do with one catalog.
type BulkRequest struct {
	Action   string `json:"action" enum:"add,remove,enable,disable"`
	ModuleID int64  `json:"moduleId"`
	SourceID string `json:"sourceId"`
	// SeriesIDs picks series explicitly; empty means every series that passes
	// the filters below.
	SeriesIDs []int64 `json:"seriesIds,omitempty"`
	TagID     int64   `json:"tagId,omitempty"`
	// RootFolderID limits it to one root folder.
	RootFolderID int64 `json:"rootFolderId,omitempty"`
	// MonitoredOnly leaves unmonitored series alone.
	MonitoredOnly bool `json:"monitoredOnly,omitempty"`
}

// BulkResult is what happened (or would happen) to one series.
type BulkResult struct {
	SeriesID int64  `json:"seriesId"`
	Title    string `json:"title"`
	// Done is what was (or would be) done: added, removed, enabled, disabled
	// or skipped.
	Done string `json:"done"`
	// Match is the title found at the catalog, with how sure the match is.
	Match string  `json:"match,omitempty"`
	URL   string  `json:"url,omitempty"`
	Score float64 `json:"score,omitempty"`
	// Reason says why nothing was done.
	Reason string `json:"reason,omitempty"`
}

// PreviewLimit caps how many series a dry run looks at: finding each one at a
// catalog is a search per series.
const PreviewLimit = 25

// BulkCount is how many series a request would touch.
func (s *Service) BulkCount(ctx context.Context, req BulkRequest) (int, error) {
	list, err := s.bulkSeries(ctx, req)
	return len(list), err
}

// BulkSources applies req to the selected series. With dryRun it only reports
// what it would do. progress (optional) is called as it goes.
func (s *Service) BulkSources(ctx context.Context, req BulkRequest, dryRun bool, progress func(done, total int)) ([]BulkResult, error) {
	if req.ModuleID == 0 || req.SourceID == "" {
		return nil, ValidationError{"pick a catalog"}
	}
	list, err := s.bulkSeries(ctx, req)
	if err != nil {
		return nil, err
	}
	if dryRun && len(list) > PreviewLimit {
		list = list[:PreviewLimit]
	}
	out := make([]BulkResult, 0, len(list))
	for i, ser := range list {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		res := s.bulkOne(ctx, req, ser, dryRun)
		out = append(out, res)
		if progress != nil {
			progress(i+1, len(list))
		}
	}
	return out, nil
}

// bulkSeries is the series a request selects, by title.
func (s *Service) bulkSeries(ctx context.Context, req BulkRequest) ([]model.Series, error) {
	q := s.db.NewSelect().Model((*model.Series)(nil)).Order("sort_title")
	if len(req.SeriesIDs) > 0 {
		q = q.Where("id IN (?)", bun.In(req.SeriesIDs))
	}
	if req.RootFolderID > 0 {
		q = q.Where("root_folder_id = ?", req.RootFolderID)
	}
	if req.MonitoredOnly {
		q = q.Where("monitored = ?", true)
	}
	var list []model.Series
	if err := q.Scan(ctx, &list); err != nil {
		return nil, err
	}
	if req.TagID > 0 {
		kept := list[:0]
		for _, ser := range list {
			for _, t := range ser.Tags {
				if t == req.TagID {
					kept = append(kept, ser)
					break
				}
			}
		}
		list = kept
	}
	return list, nil
}

func (s *Service) bulkOne(ctx context.Context, req BulkRequest, ser model.Series, dryRun bool) BulkResult {
	res := BulkResult{SeriesID: ser.ID, Title: ser.Title, Done: "skipped"}
	var link model.SeriesSource
	err := s.db.NewSelect().Model(&link).Where("series_id = ? AND module_id = ? AND source_id = ?", ser.ID, req.ModuleID, req.SourceID).
		Limit(1).Scan(ctx)
	linked := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		res.Reason = err.Error()
		return res
	}

	switch req.Action {
	case BulkRemove:
		if !linked {
			res.Reason = "not linked to this catalog"
			return res
		}
		if !dryRun {
			if err := s.UnlinkSource(ctx, ser.ID, link.ID); err != nil {
				res.Reason = err.Error()
				return res
			}
		}
		res.Done, res.Match = "removed", link.Title
		return res

	case BulkEnable, BulkDisable:
		want := req.Action == BulkEnable
		if !linked {
			res.Reason = "not linked to this catalog"
			return res
		}
		if link.Enabled == want {
			res.Reason = "already " + req.Action + "d"
			return res
		}
		if !dryRun {
			if _, err := s.UpdateSource(ctx, ser.ID, link.ID, SourceUpdate{Enabled: &want}); err != nil {
				res.Reason = err.Error()
				return res
			}
		}
		res.Done, res.Match = req.Action+"d", link.Title
		return res
	}

	// add: find the series at the catalog first
	if linked {
		res.Reason = "already linked to this catalog"
		return res
	}
	if s.search == nil {
		res.Reason = "searching catalogs isn't available"
		return res
	}
	titles := append([]string{ser.Title}, ser.Metadata.AltTitles...)
	match, err := s.search.Quick(ctx, sourcesearch.QuickSearchInput{Query: ser.Title, Titles: titles,
		Sources: []string{fmt.Sprintf("%d:%s", req.ModuleID, req.SourceID)}}, sourcesearch.QuickOptions{})
	if err != nil {
		res.Reason = err.Error()
		return res
	}
	if match == nil || match.Match == nil {
		res.Reason = "no confident match at this catalog"
		return res
	}
	m := match.Match
	res.Match, res.URL, res.Score = m.Manga.Title, m.Manga.URL, m.Score
	if !dryRun {
		if _, err := s.LinkSource(ctx, ser.ID, SourceLink{ModuleID: m.ModuleID, SourceID: m.SourceID, URL: m.Manga.URL,
			EngineRef: m.Manga.EngineRef, Title: m.Manga.Title, SourceName: m.SourceName, Lang: m.Lang}); err != nil {
			res.Done, res.Reason = "skipped", err.Error()
			return res
		}
	}
	res.Done = "added"
	return res
}

// BulkSummary counts what a run did, for a command's message.
func BulkSummary(results []BulkResult) string {
	counts := map[string]int{}
	for _, r := range results {
		counts[r.Done]++
	}
	var parts []string
	for _, k := range []string{"added", "removed", "enabled", "disabled", "skipped"} {
		if counts[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[k], k))
		}
	}
	if len(parts) == 0 {
		return "nothing to do"
	}
	return strings.Join(parts, ", ")
}
