package reading

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/Asion001/mangarr/internal/history"
	"github.com/Asion001/mangarr/internal/model"
)

// ReadAhead makes sure the chapters after a reader's position in a series
// get downloaded (reading.readAhead): the next N chapters after the
// furthest one the reader has progress on are monitored, even in series
// that aren't, and searched. It returns how many downloads were queued.
func (s *Service) ReadAhead(ctx context.Context, readerID, seriesID int64) (int, error) {
	cfg, err := s.Settings.Reading(ctx)
	if err != nil || !cfg.ReadAhead.Enabled || cfg.ReadAhead.Chapters <= 0 || s.Downloads == nil {
		return 0, err
	}
	var furthest sql.NullFloat64
	if err := s.DB.NewSelect().TableExpr("chapter_read_states AS rs").Join("JOIN chapters AS c ON c.id = rs.chapter_id").
		ColumnExpr("MAX(c.number_sort)").Where("rs.reader_id = ? AND rs.series_id = ?", readerID, seriesID).
		Where("(rs.completed OR rs.page > 0)").Scan(ctx, &furthest); err != nil || !furthest.Valid {
		return 0, err
	}
	var next []model.Chapter
	if err := s.DB.NewSelect().Model(&next).Where("series_id = ? AND number_sort > ?", seriesID, furthest.Float64).
		Order("number_sort", "id").Limit(cfg.ReadAhead.Chapters).Scan(ctx); err != nil {
		return 0, err
	}
	var want, newlyMonitored []int64
	var numbers []string
	for _, c := range next {
		if c.FileID != nil {
			continue
		}
		want = append(want, c.ID)
		if !c.Monitored {
			newlyMonitored = append(newlyMonitored, c.ID)
			numbers = append(numbers, c.NumberKey)
		}
	}
	if len(want) == 0 {
		return 0, nil
	}
	if len(newlyMonitored) > 0 {
		if _, err := s.DB.NewUpdate().Model((*model.Chapter)(nil)).Set("monitored = ?", true).Set("updated_at = ?", time.Now().UTC()).
			Where("id IN (?)", bun.In(newlyMonitored)).Exec(ctx); err != nil {
			return 0, err
		}
		_ = history.Record(ctx, s.DB, seriesID, nil, model.HistoryReadAhead, "",
			map[string]string{"chapters": strings.Join(numbers, ", "), "after": fmt.Sprint(furthest.Float64)})
		s.Bus.Changed("chapter", "updated", 0)
		s.Bus.Changed("series", "updated", seriesID)
	}
	n, err := s.Downloads.Evaluate(ctx, seriesID, want, true)
	if n > 0 {
		s.Log.Info("read ahead: downloads queued", "series", seriesID, "chapters", n)
	}
	return n, err
}
