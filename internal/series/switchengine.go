package series

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/source"
)

// SwitchRequest moves a library's links from one source module to another —
// off Suwayomi and onto mangarr's own sites, usually.
type SwitchRequest struct {
	FromModuleID int64 `json:"fromModuleId"`
	ToModuleID   int64 `json:"toModuleId"`
}

// SwitchRow is one catalog's worth of links and what would become of them.
// Links move when both modules know the catalog by the same id, which is the
// case wherever a site is built in under the id its Keiyoushi extension has.
type SwitchRow struct {
	SourceID string `json:"sourceId"`
	Catalog  string `json:"catalog"`
	// Links and Series are how much of the library this row covers.
	Links  int `json:"links"`
	Series int `json:"series"`
	// Moves says whether these links can be re-pointed.
	Moves bool `json:"moves"`
	// Reason says why they can't.
	Reason string `json:"reason,omitempty"`
}

// SwitchPlan reports what switching would do, catalog by catalog.
func (s *Service) SwitchPlan(ctx context.Context, req SwitchRequest) ([]SwitchRow, error) {
	links, known, err := s.switchState(ctx, req)
	if err != nil {
		return nil, err
	}
	byName := map[string]string{}
	for id, name := range known {
		byName[strings.ToLower(name)] = id
	}
	type group struct {
		row    SwitchRow
		series map[int64]bool
	}
	groups := map[string]*group{}
	for _, l := range links {
		g, ok := groups[l.SourceID]
		if !ok {
			g = &group{row: SwitchRow{SourceID: l.SourceID, Catalog: l.SourceName}, series: map[int64]bool{}}
			groups[l.SourceID] = g
		}
		g.row.Links++
		g.series[l.SeriesID] = true
	}
	out := make([]SwitchRow, 0, len(groups))
	for id, g := range groups {
		r := g.row
		r.Series = len(g.series)
		switch {
		case known[id] != "":
			r.Moves = true
			r.Catalog = known[id]
		case byName[strings.ToLower(r.Catalog)] != "":
			r.Reason = "the same site is built in under a different id: link it again by hand"
		default:
			r.Reason = "no site with this id"
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Moves != out[j].Moves {
			return out[i].Moves
		}
		return out[i].Links > out[j].Links
	})
	return out, nil
}

// SwitchSources re-points every link a plan says can move. Engine ids are
// dropped on the way (UpdateSource does it): they belong to the module that
// issued them, and (catalog, url) is what identifies a manga.
func (s *Service) SwitchSources(ctx context.Context, req SwitchRequest, progress func(done, total int)) (int, error) {
	links, known, err := s.switchState(ctx, req)
	if err != nil {
		return 0, err
	}
	var todo []model.SeriesSource
	for _, l := range links {
		if known[l.SourceID] != "" {
			todo = append(todo, l)
		}
	}
	moved := 0
	for i, l := range todo {
		if err := ctx.Err(); err != nil {
			return moved, err
		}
		if _, err := s.UpdateSource(ctx, l.SeriesID, l.ID, SourceUpdate{ModuleID: &req.ToModuleID}); err != nil {
			s.log.Warn("could not switch a source", "series", l.SeriesID, "link", l.ID, "err", err)
			continue
		}
		moved++
		if progress != nil {
			progress(i+1, len(todo))
		}
	}
	return moved, nil
}

// switchState is the links to move and the catalogs the target module has.
func (s *Service) switchState(ctx context.Context, req SwitchRequest) ([]model.SeriesSource, map[string]string, error) {
	if req.FromModuleID == 0 || req.ToModuleID == 0 || req.FromModuleID == req.ToModuleID {
		return nil, nil, ValidationError{"pick two different source modules"}
	}
	mod, _, err := modules.GetAs[source.Module](s.mods, req.ToModuleID)
	if err != nil {
		return nil, nil, ValidationError{err.Error()}
	}
	cats, err := mod.Sources(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list the catalogs of the module to switch to: %w", err)
	}
	known := make(map[string]string, len(cats))
	for _, c := range cats {
		name := c.DisplayName
		if name == "" {
			name = c.Name
		}
		known[c.ID] = name
	}
	var links []model.SeriesSource
	if err := s.db.NewSelect().Model(&links).Where("module_id = ?", req.FromModuleID).Order("series_id", "priority").Scan(ctx); err != nil {
		return nil, nil, err
	}
	return links, known, nil
}

// SwitchSummary counts what a plan would move, for a command's message.
func SwitchSummary(rows []SwitchRow) string {
	moving, staying := 0, 0
	for _, r := range rows {
		if r.Moves {
			moving += r.Links
			continue
		}
		staying += r.Links
	}
	if staying == 0 {
		return fmt.Sprintf("%d links move", moving)
	}
	return fmt.Sprintf("%d links move, %d stay", moving, staying)
}
