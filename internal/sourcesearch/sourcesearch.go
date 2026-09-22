// Package sourcesearch searches catalogs through the source cache: plain
// per-catalog searches, cached manga details and the quick search that goes
// through catalogs one by one until a title matches confidently. The API and
// the backup importer share it.
package sourcesearch

import (
	"context"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/Asion001/mangarr/internal/catalogs"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/settings"
	"github.com/Asion001/mangarr/internal/sourcecache"
	"github.com/Asion001/mangarr/internal/sourcegov"
	"github.com/Asion001/mangarr/internal/titlematch"
)

const (
	// SearchTTL is how long search results stay cached.
	SearchTTL = 15 * time.Minute
	// DetailsTTL is how long manga details stay cached.
	DetailsTTL = 60 * time.Minute
)

type Service struct {
	Catalogs *catalogs.Service
	Cache    *sourcecache.Cache
	Modules  *modules.Manager
	Settings *settings.Store
}

// SearchResultGroup holds one catalog's results.
type SearchResultGroup struct {
	ModuleID   int64          `json:"moduleId"`
	SourceID   string         `json:"sourceId"`
	SourceName string         `json:"sourceName"`
	Lang       string         `json:"lang"`
	Results    []source.Manga `json:"results"`
	HasNext    bool           `json:"hasNext"`
	Error      string         `json:"error,omitempty"`
	// Cached is true when the results came from the cache.
	Cached bool `json:"cached"`
}

// Search searches one catalog through the cache.
func (s *Service) Search(ctx context.Context, moduleID int64, sourceID, query string, page int) (*source.MangaPage, bool, error) {
	key := sourcecache.SearchKey(s.Catalogs.Generation(), moduleID, sourceID, query, page)
	return sourcecache.Do(s.Cache, key, SearchTTL, func() (*source.MangaPage, error) {
		mod, _, err := modules.GetAs[source.Module](s.Modules, moduleID)
		if err != nil {
			return nil, err
		}
		return mod.Search(ctx, sourceID, query, page)
	})
}

// Details fetches details and chapters through the cache.
func (s *Service) Details(ctx context.Context, moduleID int64, ref source.MangaRef, fresh bool) (*sourcecache.Details, bool, error) {
	key := sourcecache.DetailsKey(s.Catalogs.Generation(), moduleID, ref.SourceID, ref.URL)
	if fresh {
		s.Cache.DeletePrefix(key)
	}
	return sourcecache.Do(s.Cache, key, DetailsTTL, func() (*sourcecache.Details, error) {
		mod, _, err := modules.GetAs[source.Module](s.Modules, moduleID)
		if err != nil {
			return nil, err
		}
		det, chs, err := mod.Manga(ctx, ref, true)
		if err != nil {
			return nil, err
		}
		if chs == nil {
			chs = []source.Chapter{}
		}
		return &sourcecache.Details{Details: det, Chapters: chs, FetchedAt: time.Now()}, nil
	})
}

// SearchMany runs a query against many catalogs in parallel.
func (s *Service) SearchMany(ctx context.Context, query string, targets []catalogs.Catalog, page int) []SearchResultGroup {
	results := make([]SearchResultGroup, len(targets))
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			g := SearchResultGroup{ModuleID: t.ModuleID, SourceID: t.ID, SourceName: t.DisplayName, Lang: t.Lang, Results: []source.Manga{}}
			sctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			res, cached, err := s.Search(sctx, t.ModuleID, t.ID, query, page)
			cancel()
			if err == nil {
				g.Results, g.HasNext, g.Cached = res.Mangas, res.HasNext, cached
			} else {
				g.Error = err.Error()
			}
			results[i] = g
		}()
	}
	wg.Wait()
	return results
}

type QuickSearchInput struct {
	RootFolderID int64  `json:"rootFolderId,omitempty"`
	Query        string `json:"query" minLength:"1"`
	// Titles are other names of the series (metadata title, alternative titles).
	Titles []string `json:"titles,omitempty"`
	Scope  string   `json:"scope,omitempty" enum:"active,all,"`
	// Sources picks exact catalogs (moduleId:sourceId) instead of a scope.
	Sources []string `json:"sources,omitempty"`
	Lang    string   `json:"lang,omitempty"`
	// Exclude skips these catalogs (moduleId:sourceId), e.g. the ones a
	// series is already linked to when looking for another source.
	Exclude []string `json:"exclude,omitempty"`
}

// ChapterSummary describes a manga's chapter list.
type ChapterSummary struct {
	Count         int        `json:"count"`
	LatestName    string     `json:"latestName,omitempty"`
	LatestNumber  float64    `json:"latestNumber,omitempty"`
	LatestUpload  *time.Time `json:"latestUpload,omitempty"`
	Scanlators    []string   `json:"scanlators,omitempty"`
	Status        string     `json:"status,omitempty"`
	Description   string     `json:"description,omitempty"`
	DetailsCached bool       `json:"detailsCached"`
}

// QuickCandidate is one scored search result.
type QuickCandidate struct {
	ModuleID   int64           `json:"moduleId"`
	SourceID   string          `json:"sourceId"`
	SourceName string          `json:"sourceName"`
	Lang       string          `json:"lang"`
	Manga      source.Manga    `json:"manga"`
	Score      float64         `json:"score"`
	Chapters   *ChapterSummary `json:"chapters,omitempty"`
}

// QuickSearched reports what happened at one catalog.
type QuickSearched struct {
	Key        string  `json:"key"`
	SourceName string  `json:"sourceName"`
	Results    int     `json:"results"`
	BestScore  float64 `json:"bestScore"`
	Error      string  `json:"error,omitempty"`
	Cached     bool    `json:"cached"`
}

type QuickSearchResult struct {
	// Match is the first confident match (nil when none was found).
	Match *QuickCandidate `json:"match,omitempty"`
	// Top lists the best-scoring results of the searched catalogs.
	Top []QuickCandidate `json:"top"`
	// Searched are the catalogs searched in order; Remaining weren't searched.
	Searched  []QuickSearched `json:"searched"`
	Remaining []string        `json:"remaining"`
	// Groups holds the results of the searched catalogs (for the full grid).
	Groups     []SearchResultGroup `json:"groups"`
	Threshold  float64             `json:"threshold"`
	Generation int64               `json:"generation"`
}

// Summarize describes cached details.
func Summarize(d *sourcecache.Details, cached bool) *ChapterSummary {
	if d == nil {
		return nil
	}
	cs := &ChapterSummary{Count: len(d.Chapters), DetailsCached: cached}
	if d.Details != nil {
		cs.Status, cs.Description = d.Details.Status, d.Details.Description
	}
	seen := map[string]bool{}
	for _, c := range d.Chapters {
		if c.Number > cs.LatestNumber || cs.LatestName == "" {
			cs.LatestNumber, cs.LatestName = c.Number, c.Name
		}
		if c.UploadDate != nil && (cs.LatestUpload == nil || c.UploadDate.After(*cs.LatestUpload)) {
			cs.LatestUpload = c.UploadDate
		}
		if c.Scanlator != "" && !seen[c.Scanlator] && len(cs.Scanlators) < 3 {
			seen[c.Scanlator] = true
			cs.Scanlators = append(cs.Scanlators, c.Scanlator)
		}
	}
	return cs
}

// QuickOptions tune a quick search beyond the sources settings.
type QuickOptions struct {
	// Budget overrides the settings' time budget.
	Budget time.Duration
	// Threshold overrides the settings' match threshold.
	Threshold float64
}

// Quick searches catalogs one by one in priority order and stops at the
// first result whose title matches confidently.
func (s *Service) Quick(ctx context.Context, in QuickSearchInput, opt QuickOptions) (*QuickSearchResult, error) {
	st, err := s.Settings.Sources(ctx)
	if err != nil {
		return nil, err
	}
	qs := st.QuickSearch
	threshold := qs.Threshold
	if opt.Threshold > 0 {
		threshold = opt.Threshold
	}
	if threshold <= 0 || threshold > 1 {
		threshold = 0.88
	}
	budget := time.Duration(max(qs.BudgetSeconds, 5)) * time.Second
	if opt.Budget > 0 {
		budget = opt.Budget
	}
	deadline := time.Now().Add(budget)

	scope := catalogs.Scope(in.Scope)
	if scope == "" {
		scope = catalogs.ScopeActive
	}
	targets, _ := s.Catalogs.Select(ctx, catalogs.Filter{Scope: scope, Lang: in.Lang, Keys: in.Sources, RootFolderID: in.RootFolderID})
	if len(in.Exclude) > 0 {
		targets = slices.DeleteFunc(targets, func(c catalogs.Catalog) bool { return slices.Contains(in.Exclude, c.Key()) })
	}
	titles := append([]string{in.Query}, in.Titles...)
	res := &QuickSearchResult{Top: []QuickCandidate{}, Searched: []QuickSearched{}, Remaining: []string{}, Groups: []SearchResultGroup{},
		Threshold: threshold, Generation: s.Catalogs.Generation()}

	topN := 0
	switch qs.Details {
	case "best":
		topN = 1
	case "top":
		topN = min(max(qs.TopN, 1), 5)
	}
	withDetails := func(c *QuickCandidate) {
		dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		d, cached, err := s.Details(dctx, c.ModuleID, c.Manga.MangaRef, false)
		if err == nil {
			c.Chapters = Summarize(d, cached)
		}
	}

	same := func(a, b *QuickCandidate) bool {
		return a != nil && b != nil && a.ModuleID == b.ModuleID && a.SourceID == b.SourceID && a.Manga.URL == b.Manga.URL
	}
	var all []QuickCandidate
	for i, t := range targets {
		if res.Match != nil || time.Now().After(deadline) {
			for _, r := range targets[i:] {
				res.Remaining = append(res.Remaining, r.Key())
			}
			break
		}
		sr := QuickSearched{Key: t.Key(), SourceName: t.DisplayName}
		g := SearchResultGroup{ModuleID: t.ModuleID, SourceID: t.ID, SourceName: t.DisplayName, Lang: t.Lang, Results: []source.Manga{}}
		if t.CooldownUntil != nil {
			sr.Error = (&sourcegov.ErrCoolingDown{Until: *t.CooldownUntil, Reason: t.CooldownReason}).Error()
			g.Error = sr.Error
			res.Searched, res.Groups = append(res.Searched, sr), append(res.Groups, g)
			continue
		}
		sctx, cancel := context.WithDeadline(ctx, deadline)
		page, cached, err := s.Search(sctx, t.ModuleID, t.ID, in.Query, 1)
		cancel()
		if err != nil {
			sr.Error, g.Error = err.Error(), err.Error()
		} else {
			sr.Results, sr.Cached = len(page.Mangas), cached
			g.Results, g.HasNext, g.Cached = page.Mangas, page.HasNext, cached
			var confident []int
			for _, m := range page.Mangas {
				c := QuickCandidate{ModuleID: t.ModuleID, SourceID: t.ID, SourceName: t.DisplayName, Lang: t.Lang, Manga: m,
					Score: titlematch.Best(titles, m.Title)}
				sr.BestScore = max(sr.BestScore, c.Score)
				if c.Score >= threshold {
					confident = append(confident, len(all))
				}
				all = append(all, c)
			}
			sort.SliceStable(confident, func(i, j int) bool { return all[confident[i]].Score > all[confident[j]].Score })
			for n, idx := range confident {
				c := &all[idx]
				if topN == 0 {
					res.Match = c
					break
				}
				if n >= 3 {
					break
				}
				// a title match without chapters (e.g. licensed and removed)
				// isn't what the user wants: try the next one or catalog
				withDetails(c)
				if c.Chapters == nil || c.Chapters.Count > 0 {
					res.Match = c
					break
				}
			}
			if res.Match != nil {
				m := *res.Match
				res.Match = &m
			}
		}
		res.Searched, res.Groups = append(res.Searched, sr), append(res.Groups, g)
	}

	sort.SliceStable(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	for i := 0; i < len(all) && i < max(topN, 5); i++ {
		c := all[i]
		switch {
		case same(&c, res.Match):
			c.Chapters = res.Match.Chapters
		case i < topN && c.Chapters == nil:
			withDetails(&c)
		}
		res.Top = append(res.Top, c)
	}
	return res, nil
}
