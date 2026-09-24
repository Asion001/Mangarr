package api

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/catalogs"
	"github.com/Asion001/mangarr/internal/model"
)

// CatalogHealth is how a catalog is doing for the series that link it.
type CatalogHealth struct {
	ModuleID int64  `json:"moduleId"`
	SourceID string `json:"sourceId"`
	// Series links this catalog; Failing of those links are switched on and
	// failed their last check.
	Series  int `json:"series"`
	Failing int `json:"failing"`
	// LastSuccessAt and LastCheckedAt are the latest of any link.
	LastSuccessAt *time.Time `json:"lastSuccessAt,omitempty"`
	LastCheckedAt *time.Time `json:"lastCheckedAt,omitempty"`
}

func init() { register((*Server).registerCatalogHealth) }

func (s *Server) registerCatalogHealth() {
	huma.Register(s.api, huma.Operation{OperationID: "catalogs-health", Method: http.MethodGet, Path: "/api/v1/catalogs/health", Tags: []string{"Catalogs"},
		Summary: "How many series use each catalog, how many of those links are failing, and when it last worked"},
		func(ctx context.Context, _ *struct{}) (*struct{ Body []CatalogHealth }, error) {
			var links []model.SeriesSource
			err := s.app.DB.NewSelect().Model(&links).
				Column("series_id", "module_id", "source_id", "enabled", "consecutive_failures", "last_success_at", "last_checked_at").Scan(ctx)
			if err != nil {
				return nil, toHTTPError(err)
			}
			return &struct{ Body []CatalogHealth }{catalogHealth(links)}, nil
		})
}

func catalogHealth(links []model.SeriesSource) []CatalogHealth {
	by := map[string]*CatalogHealth{}
	seen := map[string]map[int64]bool{}
	later := func(a, b *time.Time) *time.Time {
		if a == nil || (b != nil && b.After(*a)) {
			return b
		}
		return a
	}
	for _, l := range links {
		k := catalogs.Key(l.ModuleID, l.SourceID)
		h := by[k]
		if h == nil {
			h = &CatalogHealth{ModuleID: l.ModuleID, SourceID: l.SourceID}
			by[k], seen[k] = h, map[int64]bool{}
		}
		if !seen[k][l.SeriesID] {
			seen[k][l.SeriesID] = true
			h.Series++
		}
		if l.Enabled && l.ConsecutiveFailures > 0 {
			h.Failing++
		}
		h.LastSuccessAt = later(h.LastSuccessAt, l.LastSuccessAt)
		h.LastCheckedAt = later(h.LastCheckedAt, l.LastCheckedAt)
	}
	out := make([]CatalogHealth, 0, len(by))
	for _, h := range by {
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool {
		return catalogs.Key(out[i].ModuleID, out[i].SourceID) < catalogs.Key(out[j].ModuleID, out[j].SourceID)
	})
	return out
}
