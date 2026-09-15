package reading

import (
	"context"
	"sort"
	"time"
)

// NextUp is the next chapter to read in a series.
type NextUp struct {
	Series SeriesInfo
	Book   BookInfo
}

// OnDeck lists, for each series the reader has started and not finished,
// the first unread chapter after the last one read, most recently read
// series first.
func (s *Service) OnDeck(ctx context.Context, readerID int64) ([]NextUp, error) {
	series, err := s.AllSeries(ctx, readerID, 0)
	if err != nil {
		return nil, err
	}
	started := map[int64]SeriesInfo{}
	for _, si := range series {
		if (si.Read > 0 || si.InProgress > 0) && si.Read < si.Books {
			started[si.Series.ID] = si
		}
	}
	if len(started) == 0 {
		return []NextUp{}, nil
	}
	books, err := s.Books(ctx, readerID, 0)
	if err != nil {
		return nil, err
	}
	bySeries := map[int64][]BookInfo{}
	for _, b := range books {
		if _, ok := started[b.Chapter.SeriesID]; ok {
			bySeries[b.Chapter.SeriesID] = append(bySeries[b.Chapter.SeriesID], b)
		}
	}
	out := []NextUp{}
	for sid, list := range bySeries { // list is ordered by number
		last := -1
		for i, b := range list {
			if b.State != nil && b.State.Completed {
				last = i
			}
		}
		for i := last + 1; i < len(list); i++ {
			if list[i].State == nil || !list[i].State.Completed {
				out = append(out, NextUp{Series: started[sid], Book: list[i]})
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return lastRead(out[i].Series).After(lastRead(out[j].Series))
	})
	return out, nil
}

func lastRead(si SeriesInfo) time.Time {
	if si.LastRead == nil {
		return time.Time{}
	}
	return *si.LastRead
}
