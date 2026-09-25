package downloads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/model"
)

const rankStep int64 = 1 << 20

// Keep ranks exactly representable by JSON clients as well as SQL BIGINT.
const rankLimit int64 = 1 << 52

var pendingStatuses = []string{model.JobQueued, model.JobPaused}

// orderTx serializes rank allocation across processes.
// SQLite begins IMMEDIATE transactions; this singleton also locks PostgreSQL.
func (q *Queue) orderTx(ctx context.Context, fn func(context.Context, bun.Tx) error) error {
	return q.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE download_queue_order SET revision = revision WHERE id = 1"); err != nil {
			return err
		}
		return fn(ctx, tx)
	})
}

func bumpOrder(ctx context.Context, tx bun.Tx) error {
	_, err := tx.ExecContext(ctx, "UPDATE download_queue_order SET revision = revision + 1 WHERE id = 1")
	return err
}

// ranks reserves a gap in the global order, including finished jobs so retries
// retain their position. Only exhausted gaps require a full, order-preserving
// renumber. Callers hold orderTx until their inserts/updates have committed.
func ranks(ctx context.Context, tx bun.Tx, action string, anchorID int64, n int) ([]int64, error) {
	for attempt := 0; attempt < 2; attempt++ {
		var low, high int64
		var edge sql.NullInt64
		switch action {
		case "top", "bottom":
			agg := "MIN(rank)"
			if action == "bottom" {
				agg = "MAX(rank)"
			}
			if err := tx.NewSelect().Table("download_jobs").ColumnExpr(agg).Scan(ctx, &edge); err != nil {
				return nil, err
			}
			if action == "top" {
				high = edge.Int64
				low = high - rankStep*int64(n+1)
			} else {
				low = edge.Int64
				high = low + rankStep*int64(n+1)
			}
		case "before", "after":
			var anchor int64
			if err := tx.NewSelect().Table("download_jobs").Column("rank").Where("id = ?", anchorID).Scan(ctx, &anchor); err != nil {
				return nil, err
			}
			if action == "before" {
				high = anchor
				if err := tx.NewSelect().Table("download_jobs").ColumnExpr("MAX(rank)").Where("rank < ?", anchor).Scan(ctx, &edge); err != nil {
					return nil, err
				}
				low = edge.Int64
				if !edge.Valid {
					low = high - rankStep*int64(n+1)
				}
			} else {
				low = anchor
				if err := tx.NewSelect().Table("download_jobs").ColumnExpr("MIN(rank)").Where("rank > ?", anchor).Scan(ctx, &edge); err != nil {
					return nil, err
				}
				high = edge.Int64
				if !edge.Valid {
					high = low + rankStep*int64(n+1)
				}
			}
		default:
			return nil, fmt.Errorf("unknown move %q", action)
		}
		step := (high - low) / int64(n+1)
		if low > -rankLimit && high < rankLimit && step > 0 {
			out := make([]int64, n)
			for i := range out {
				out[i] = low + step*int64(i+1)
			}
			return out, nil
		}
		if _, err := tx.ExecContext(ctx, `WITH ordered AS (
   SELECT id, ROW_NUMBER() OVER (ORDER BY rank, id) * ? AS new_rank FROM download_jobs
  ) UPDATE download_jobs SET rank = (SELECT new_rank FROM ordered WHERE ordered.id = download_jobs.id)`, rankStep); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("queue rank space exhausted")
}

// insertionRanks translates the legacy enqueue priority into a rank once.
// Manual moves subsequently control order independently of that priority.
func insertionRanks(ctx context.Context, tx bun.Tx, priority, n int) ([]int64, error) {
	var anchor int64
	err := tx.NewSelect().Table("download_jobs").Column("id").
		Where("status IN (?) AND priority < ?", bun.In(pendingStatuses), priority).
		OrderExpr("rank, id").Limit(1).Scan(ctx, &anchor)
	if errors.Is(err, sql.ErrNoRows) {
		return ranks(ctx, tx, "bottom", 0, n)
	}
	if err != nil {
		return nil, err
	}
	return ranks(ctx, tx, "before", anchor, n)
}

// Move reorders only queued/paused jobs, preserving their current relative
// order regardless of request ID order. Both kinds share one rank namespace.
// A before/after anchor must be pending and outside the selection. Missing or
// running selected IDs are ignored, as in the legacy bulk endpoints.
func (q *Queue) Move(ctx context.Context, ids []int64, action string, anchorID int64) (int, error) {
	switch action {
	case "top", "bottom":
		if anchorID != 0 {
			return 0, errors.New("top/bottom do not accept an anchor")
		}
	case "before", "after":
		if anchorID <= 0 {
			return 0, errors.New("before/after require anchorId")
		}
		for _, id := range ids {
			if id == anchorID {
				return 0, errors.New("anchor must not be selected")
			}
		}
	default:
		return 0, fmt.Errorf("unknown move %q", action)
	}
	affected := 0
	err := q.orderTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		if anchorID != 0 {
			var anchor model.DownloadJob
			sel := tx.NewSelect().Model(&anchor).Where("id = ? AND status IN (?)", anchorID, bun.In(pendingStatuses))
			if q.db.Kind == db.Postgres {
				sel = sel.For("UPDATE")
			}
			if err := sel.Scan(ctx); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return errors.New("anchor must be a pending job")
				}
				return err
			}
		}
		if len(ids) == 0 {
			return nil
		}
		var jobs []model.DownloadJob
		sel := tx.NewSelect().Model(&jobs).Where("id IN (?) AND status IN (?)", bun.In(ids), bun.In(pendingStatuses)).OrderExpr("rank, id")
		if q.db.Kind == db.Postgres {
			sel = sel.For("UPDATE")
		}
		if err := sel.Scan(ctx); err != nil {
			return err
		}
		if len(jobs) == 0 {
			return nil
		}
		values, err := ranks(ctx, tx, action, anchorID, len(jobs))
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		for i, job := range jobs {
			if _, err := tx.NewUpdate().Model((*model.DownloadJob)(nil)).Set("rank = ?", values[i]).Set("updated_at = ?", now).Where("id = ?", job.ID).Exec(ctx); err != nil {
				return err
			}
		}
		affected = len(jobs)
		return bumpOrder(ctx, tx)
	})
	if err != nil {
		return 0, err
	}
	if affected > 0 {
		q.signal()
		q.bus.Changed("queue", "sync", 0)
		q.bus.Changed("chapter", "updated", 0)
	}
	return affected, nil
}
