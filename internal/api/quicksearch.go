package api

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/catalogs"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/sourcecache"
	"github.com/Asion001/mangarr/internal/sourcegov"
	"github.com/Asion001/mangarr/internal/titlematch"
)

type QuickSearchInput struct {
	Query string `json:"query" minLength:"1"`
	// Titles are other names of the series (metadata title, alternative titles).
	Titles []string `json:"titles,omitempty"`
	Scope  string   `json:"scope,omitempty" enum:"active,all,"`
	// Sources picks exact catalogs (moduleId:sourceId) instead of a scope.
	Sources []string `json:"sources,omitempty"`
	Lang    string   `json:"lang,omitempty"`
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

func summarize(d *sourcecache.Details, cached bool) *ChapterSummary {
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

// quickSearch searches catalogs one by one in priority order and stops at
// the first result whose title matches confidently.
func (s *Server) quickSearch(ctx context.Context, in QuickSearchInput) (*QuickSearchResult, error) {
	st, err := s.app.Settings.Sources(ctx)
	if err != nil {
		return nil, err
	}
	qs := st.QuickSearch
	threshold := qs.Threshold
	if threshold <= 0 || threshold > 1 {
		threshold = 0.88
	}
	budget := time.Duration(max(qs.BudgetSeconds, 5)) * time.Second
	deadline := time.Now().Add(budget)

	scope := catalogs.Scope(in.Scope)
	if scope == "" {
		scope = catalogs.ScopeActive
	}
	targets, _ := s.app.Catalogs.Select(ctx, catalogs.Filter{Scope: scope, Lang: in.Lang, Keys: in.Sources})
	titles := append([]string{in.Query}, in.Titles...)
	res := &QuickSearchResult{Top: []QuickCandidate{}, Searched: []QuickSearched{}, Remaining: []string{}, Groups: []SearchResultGroup{},
		Threshold: threshold, Generation: s.app.Catalogs.Generation()}

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
		d, cached, err := s.mangaDetails(dctx, c.ModuleID, c.Manga.MangaRef, false)
		if err == nil {
			c.Chapters = summarize(d, cached)
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
		page, cached, err := s.searchCatalog(sctx, t.ModuleID, t.ID, in.Query, 1)
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

func (s *Server) registerQuickSearch() {
	huma.Register(s.api, huma.Operation{OperationID: "sources-quick-search", Method: http.MethodPost, Path: "/api/v1/sources/quick-search", Tags: []string{"Sources"},
		Summary: "Search catalogs one by one in priority order and stop at the first confident title match"},
		func(ctx context.Context, in *struct{ Body QuickSearchInput }) (*struct{ Body *QuickSearchResult }, error) {
			in.Body.Query = strings.TrimSpace(in.Body.Query)
			res, err := s.quickSearch(ctx, in.Body)
			if err != nil {
				return nil, toHTTPError(err)
			}
			return &struct{ Body *QuickSearchResult }{res}, nil
		})
}

func init() { register((*Server).registerQuickSearch) }
