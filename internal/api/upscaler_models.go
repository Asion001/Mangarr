package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/upscale"
)

func init() { register((*Server).registerUpscalerModels) }

// UpscalerModelCatalog is the union of models reported by every enabled
// upscaler. Remembered worker capabilities stay in the catalog while a worker
// is offline, but their location is marked unavailable.
type UpscalerModelCatalog struct {
	Models  []UpscalerModel  `json:"models"`
	Sources []UpscalerSource `json:"sources"`
}

type UpscalerModel struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Scales      []int                   `json:"scales"`
	NoiseLevels []int                   `json:"noiseLevels,omitempty"`
	Sources     []UpscalerModelLocation `json:"sources"`
}

type UpscalerModelLocation struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

type UpscalerSource struct {
	Name      string   `json:"name"`
	Available bool     `json:"available"`
	Devices   []string `json:"devices"`
	Error     string   `json:"error,omitempty"`
}

func (s *Server) registerUpscalerModels() {
	huma.Register(s.api, huma.Operation{OperationID: "upscaler-models", Method: http.MethodGet, Path: "/api/v1/upscalers/models", Tags: []string{"Profiles"},
		Summary: "Models available from enabled built-in and worker upscalers"},
		func(ctx context.Context, _ *struct{}) (*struct{ Body UpscalerModelCatalog }, error) {
			catalog, err := s.upscalerModelCatalog(ctx)
			if err != nil {
				return nil, toHTTPError(err)
			}
			return &struct{ Body UpscalerModelCatalog }{catalog}, nil
		})
}

func (s *Server) upscalerModelCatalog(ctx context.Context) (UpscalerModelCatalog, error) {
	out := UpscalerModelCatalog{Models: []UpscalerModel{}, Sources: []UpscalerSource{}}
	byName := map[string]*UpscalerModel{}
	add := func(m upscale.Model, location UpscalerModelLocation) {
		entry := byName[m.Name]
		if entry == nil {
			entry = &UpscalerModel{Name: m.Name, Description: m.Description, Scales: []int{}, Sources: []UpscalerModelLocation{}}
			byName[m.Name] = entry
		}
		if entry.Description == "" {
			entry.Description = m.Description
		}
		entry.Scales = mergeInts(entry.Scales, m.Scales)
		entry.NoiseLevels = mergeInts(entry.NoiseLevels, m.NoiseLevels)
		for _, source := range entry.Sources {
			if source.Name == location.Name {
				return
			}
		}
		entry.Sources = append(entry.Sources, location)
	}

	for _, engine := range modules.ActiveAs[upscale.Module](s.app.Modules, modules.KindUpscale) {
		if engine.Def.Implementation == "workers" {
			var workers []model.Worker
			if err := s.app.DB.NewSelect().Model(&workers).Order("name").Scan(ctx); err != nil {
				return out, err
			}
			found := false
			for _, worker := range workers {
				if !worker.Enabled || !worker.HasRole(model.RoleUpscale) {
					continue
				}
				found = true
				models := decodeUpscaleModels(worker.Info["models"])
				available := online(worker) && len(models) > 0
				devices := decodeStrings(worker.Info["devices"])
				out.Sources = append(out.Sources, UpscalerSource{Name: worker.Name, Available: available, Devices: devices})
				for _, mdl := range models {
					add(mdl, UpscalerModelLocation{Name: worker.Name, Available: available})
				}
			}
			if !found {
				out.Sources = append(out.Sources, UpscalerSource{Name: engine.Def.Name, Devices: []string{}, Error: "no upscaling workers configured"})
			}
			continue
		}
		info, err := engine.Instance.Info(ctx)
		if err != nil {
			out.Sources = append(out.Sources, UpscalerSource{Name: engine.Def.Name, Devices: []string{}, Error: err.Error()})
			continue
		}
		available := len(info.Models) > 0
		out.Sources = append(out.Sources, UpscalerSource{Name: engine.Def.Name, Available: available, Devices: append([]string{}, info.Devices...)})
		for _, mdl := range info.Models {
			add(mdl, UpscalerModelLocation{Name: engine.Def.Name, Available: available})
		}
	}
	for _, mdl := range byName {
		sort.Slice(mdl.Sources, func(i, j int) bool {
			if mdl.Sources[i].Available != mdl.Sources[j].Available {
				return mdl.Sources[i].Available
			}
			return strings.ToLower(mdl.Sources[i].Name) < strings.ToLower(mdl.Sources[j].Name)
		})
		out.Models = append(out.Models, *mdl)
	}
	sort.Slice(out.Models, func(i, j int) bool { return strings.ToLower(out.Models[i].Name) < strings.ToLower(out.Models[j].Name) })
	sort.Slice(out.Sources, func(i, j int) bool {
		if out.Sources[i].Available != out.Sources[j].Available {
			return out.Sources[i].Available
		}
		return strings.ToLower(out.Sources[i].Name) < strings.ToLower(out.Sources[j].Name)
	})
	return out, nil
}

func mergeInts(dst, src []int) []int {
	seen := make(map[int]bool, len(dst)+len(src))
	for _, n := range dst {
		seen[n] = true
	}
	for _, n := range src {
		if !seen[n] {
			dst = append(dst, n)
			seen[n] = true
		}
	}
	sort.Ints(dst)
	return dst
}

func decodeUpscaleModels(value any) []upscale.Model {
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out []upscale.Model
	if json.Unmarshal(data, &out) != nil {
		return nil
	}
	return out
}

func decodeStrings(value any) []string {
	data, err := json.Marshal(value)
	if err != nil {
		return []string{}
	}
	var out []string
	if json.Unmarshal(data, &out) != nil {
		return []string{}
	}
	return out
}
