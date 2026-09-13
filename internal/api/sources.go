package api

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/library"
	"github.com/Asion001/mangarr/internal/modules/metadata"
	"github.com/Asion001/mangarr/internal/modules/source"
)

func init() {
	capabilityProbes = append(capabilityProbes,
		func(i modules.Instance) string { _, ok := i.(source.ExtensionManager); return capIf(ok, "extensions") },
		func(i modules.Instance) string { _, ok := i.(source.Preferences); return capIf(ok, "preferences") },
		func(i modules.Instance) string { _, ok := i.(source.Latest); return capIf(ok, "browse") },
		func(i modules.Instance) string { _, ok := i.(library.ProgressReader); return capIf(ok, "progress") },
		func(i modules.Instance) string { _, ok := i.(metadata.ExternalLookup); return capIf(ok, "externalLookup") },
	)
	register((*Server).registerSources)
}

func capIf(ok bool, name string) string {
	if ok {
		return name
	}
	return ""
}

type SourceResource struct {
	ModuleID   int64  `json:"moduleId"`
	ModuleName string `json:"moduleName"`
	source.SourceInfo
}

type SearchResultGroup struct {
	ModuleID   int64          `json:"moduleId"`
	SourceID   string         `json:"sourceId"`
	SourceName string         `json:"sourceName"`
	Lang       string         `json:"lang"`
	Results    []source.Manga `json:"results"`
	HasNext    bool           `json:"hasNext"`
	Error      string         `json:"error,omitempty"`
}

type imageOutput struct {
	ContentType  string `header:"Content-Type"`
	CacheControl string `header:"Cache-Control"`
	Body         []byte
}

// sourceCatalogs caches catalog lists per module for a minute.
type catalogCache struct {
	mu   sync.Mutex
	at   map[int64]time.Time
	list map[int64][]source.SourceInfo
}

var catalogs = catalogCache{at: map[int64]time.Time{}, list: map[int64][]source.SourceInfo{}}

func (s *Server) listSources(ctx context.Context, fresh bool) ([]SourceResource, []string) {
	var out []SourceResource
	var errs []string
	for _, m := range modules.ActiveAs[source.Module](s.app.Modules, modules.KindSource) {
		catalogs.mu.Lock()
		list, ok := catalogs.list[m.Def.ID]
		if !ok || fresh || time.Since(catalogs.at[m.Def.ID]) > time.Minute {
			catalogs.mu.Unlock()
			l, err := m.Instance.Sources(ctx)
			if err != nil {
				errs = append(errs, m.Def.Name+": "+err.Error())
				continue
			}
			catalogs.mu.Lock()
			catalogs.list[m.Def.ID], catalogs.at[m.Def.ID] = l, time.Now()
			list = l
		}
		catalogs.mu.Unlock()
		for _, si := range list {
			out = append(out, SourceResource{ModuleID: m.Def.ID, ModuleName: m.Def.Name, SourceInfo: si})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Lang != out[j].Lang {
			return out[i].Lang < out[j].Lang
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, errs
}

// SearchSources runs a query against many catalogs in parallel.
func (s *Server) SearchSources(ctx context.Context, query string, targets []SourceResource, page int) []SearchResultGroup {
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
			mod, _, err := modules.GetAs[source.Module](s.app.Modules, t.ModuleID)
			if err == nil {
				sctx, cancel := context.WithTimeout(ctx, 60*time.Second)
				var res *source.MangaPage
				res, err = mod.Search(sctx, t.ID, query, page)
				cancel()
				if err == nil {
					g.Results, g.HasNext = res.Mangas, res.HasNext
				}
			}
			if err != nil {
				g.Error = err.Error()
			}
			results[i] = g
		}()
	}
	wg.Wait()
	return results
}

func (s *Server) registerSources() {
	tags := []string{"Sources"}
	huma.Register(s.api, huma.Operation{OperationID: "sources-list", Method: http.MethodGet, Path: "/api/v1/sources", Tags: tags,
		Summary: "List catalogs of all active source modules"},
		func(ctx context.Context, in *struct {
			Refresh bool `query:"refresh"`
		}) (*struct{ Body []SourceResource }, error) {
			out, errs := s.listSources(ctx, in.Refresh)
			if out == nil {
				out = []SourceResource{}
			}
			if len(out) == 0 && len(errs) > 0 {
				return nil, huma.Error502BadGateway(strings.Join(errs, "; "))
			}
			return &struct{ Body []SourceResource }{out}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "sources-search", Method: http.MethodGet, Path: "/api/v1/sources/search", Tags: tags,
		Summary: "Search several catalogs at once. Select catalogs with source=moduleId:sourceId (repeatable) or lang."},
		func(ctx context.Context, in *struct {
			Query   string   `query:"q" minLength:"1"`
			Sources []string `query:"source"`
			Lang    string   `query:"lang"`
			Page    int      `query:"page" default:"1"`
		}) (*struct{ Body []SearchResultGroup }, error) {
			all, _ := s.listSources(ctx, false)
			want := map[string]bool{}
			for _, x := range in.Sources {
				want[x] = true
			}
			var targets []SourceResource
			for _, sr := range all {
				key := strconv.FormatInt(sr.ModuleID, 10) + ":" + sr.ID
				if len(want) > 0 && !want[key] {
					continue
				}
				if in.Lang != "" && sr.Lang != in.Lang && sr.Lang != "all" {
					continue
				}
				targets = append(targets, sr)
			}
			if len(targets) > 40 {
				return nil, huma.Error400BadRequest("too many sources selected (max 40); pick sources or a language")
			}
			return &struct{ Body []SearchResultGroup }{s.SearchSources(ctx, in.Query, targets, in.Page)}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "sources-browse", Method: http.MethodGet, Path: "/api/v1/sources/{moduleId}/{sourceId}/browse", Tags: tags},
		func(ctx context.Context, in *struct {
			ModuleID int64  `path:"moduleId"`
			SourceID string `path:"sourceId"`
			Type     string `query:"type" enum:"latest,popular,search" default:"popular"`
			Query    string `query:"q"`
			Page     int    `query:"page" default:"1"`
		}) (*struct{ Body *source.MangaPage }, error) {
			mod, _, err := modules.GetAs[source.Module](s.app.Modules, in.ModuleID)
			if err != nil {
				return nil, huma.Error404NotFound(err.Error())
			}
			var res *source.MangaPage
			switch in.Type {
			case "search":
				res, err = mod.Search(ctx, in.SourceID, in.Query, in.Page)
			default:
				l, ok := mod.(source.Latest)
				if !ok {
					return nil, huma.Error400BadRequest(source.ErrUnsupported.Error())
				}
				if in.Type == "latest" {
					res, err = l.Latest(ctx, in.SourceID, in.Page)
				} else {
					res, err = l.Popular(ctx, in.SourceID, in.Page)
				}
			}
			if err != nil {
				return nil, huma.Error502BadGateway(err.Error())
			}
			return &struct{ Body *source.MangaPage }{res}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "sources-manga", Method: http.MethodGet, Path: "/api/v1/sources/{moduleId}/{sourceId}/manga", Tags: tags,
		Summary: "Fetch details and chapters of a manga at a source (preview before adding)"},
		func(ctx context.Context, in *struct {
			ModuleID  int64  `path:"moduleId"`
			SourceID  string `path:"sourceId"`
			URL       string `query:"url" minLength:"1"`
			EngineRef string `query:"engineRef"`
		}) (*struct {
			Body struct {
				Details  *source.MangaDetails `json:"details"`
				Chapters []source.Chapter     `json:"chapters"`
			}
		}, error) {
			mod, _, err := modules.GetAs[source.Module](s.app.Modules, in.ModuleID)
			if err != nil {
				return nil, huma.Error404NotFound(err.Error())
			}
			det, chs, err := mod.Manga(ctx, source.MangaRef{SourceID: in.SourceID, URL: in.URL, EngineRef: in.EngineRef}, true)
			if err != nil {
				return nil, huma.Error502BadGateway(err.Error())
			}
			out := &struct {
				Body struct {
					Details  *source.MangaDetails `json:"details"`
					Chapters []source.Chapter     `json:"chapters"`
				}
			}{}
			out.Body.Details, out.Body.Chapters = det, chs
			return out, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "sources-thumbnail", Method: http.MethodGet, Path: "/api/v1/sources/{moduleId}/{sourceId}/thumbnail", Tags: tags,
		Summary: "Proxy (and cache) a manga thumbnail through its source module"},
		func(ctx context.Context, in *struct {
			ModuleID  int64  `path:"moduleId"`
			SourceID  string `path:"sourceId"`
			URL       string `query:"url" minLength:"1"`
			EngineRef string `query:"engineRef"`
		}) (*imageOutput, error) {
			key := strconv.FormatInt(in.ModuleID, 10) + "|" + in.SourceID + "|" + in.URL
			data, ct, err := s.cachedImage(ctx, "thumbs", key, 7*24*time.Hour, func(ctx context.Context) (io.ReadCloser, string, error) {
				mod, _, err := modules.GetAs[source.Thumbnails](s.app.Modules, in.ModuleID)
				if err != nil {
					return nil, "", err
				}
				return mod.Thumbnail(ctx, source.MangaRef{SourceID: in.SourceID, URL: in.URL, EngineRef: in.EngineRef})
			})
			if err != nil {
				return nil, huma.Error404NotFound(err.Error())
			}
			return &imageOutput{ContentType: ct, CacheControl: "public, max-age=86400", Body: data}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "modules-asset", Method: http.MethodGet, Path: "/api/v1/modules/{id}/asset", Tags: tags,
		Summary: "Proxy a module-relative image (e.g. extension icons)"},
		func(ctx context.Context, in *struct {
			ID   int64  `path:"id"`
			Path string `query:"path" minLength:"1"`
		}) (*imageOutput, error) {
			key := strconv.FormatInt(in.ID, 10) + "|" + in.Path
			data, ct, err := s.cachedImage(ctx, "assets", key, 7*24*time.Hour, func(ctx context.Context) (io.ReadCloser, string, error) {
				a, _, err := modules.GetAs[source.Assets](s.app.Modules, in.ID)
				if err != nil {
					return nil, "", err
				}
				return a.FetchAsset(ctx, in.Path)
			})
			if err != nil {
				return nil, huma.Error404NotFound(err.Error())
			}
			return &imageOutput{ContentType: ct, CacheControl: "public, max-age=604800", Body: data}, nil
		})

	// ---- extensions / stores / preferences

	etags := []string{"Extensions"}
	huma.Register(s.api, huma.Operation{OperationID: "extensions-list", Method: http.MethodGet, Path: "/api/v1/modules/{id}/extensions", Tags: etags},
		func(ctx context.Context, in *struct {
			ID      int64 `path:"id"`
			Refresh bool  `query:"refresh"`
		}) (*struct{ Body []source.Extension }, error) {
			em, _, err := modules.GetAs[source.ExtensionManager](s.app.Modules, in.ID)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			list, err := em.Extensions(ctx, in.Refresh)
			if err != nil {
				return nil, huma.Error502BadGateway(err.Error())
			}
			return &struct{ Body []source.Extension }{list}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "extensions-action", Method: http.MethodPost, Path: "/api/v1/modules/{id}/extensions/{pkg}/{action}", Tags: etags},
		func(ctx context.Context, in *struct {
			ID     int64  `path:"id"`
			Pkg    string `path:"pkg"`
			Action string `path:"action" enum:"install,update,uninstall"`
		}) (*struct{}, error) {
			em, _, err := modules.GetAs[source.ExtensionManager](s.app.Modules, in.ID)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			switch in.Action {
			case "install":
				err = em.InstallExtension(ctx, in.Pkg)
			case "update":
				err = em.UpdateExtension(ctx, in.Pkg)
			case "uninstall":
				err = em.UninstallExtension(ctx, in.Pkg)
			}
			if err != nil {
				return nil, huma.Error502BadGateway(err.Error())
			}
			catalogs.mu.Lock()
			delete(catalogs.list, in.ID)
			catalogs.mu.Unlock()
			s.app.Bus.Changed("extension", "updated", in.ID)
			return nil, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "stores-list", Method: http.MethodGet, Path: "/api/v1/modules/{id}/stores", Tags: etags},
		func(ctx context.Context, in *IDPath) (*struct{ Body []string }, error) {
			em, _, err := modules.GetAs[source.ExtensionManager](s.app.Modules, in.ID)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			list, err := em.Stores(ctx)
			if err != nil {
				return nil, huma.Error502BadGateway(err.Error())
			}
			return &struct{ Body []string }{list}, nil
		})
	huma.Register(s.api, huma.Operation{OperationID: "stores-add", Method: http.MethodPost, Path: "/api/v1/modules/{id}/stores", Tags: etags},
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body struct {
				URL string `json:"url" minLength:"1"`
			}
		}) (*struct{}, error) {
			em, _, err := modules.GetAs[source.ExtensionManager](s.app.Modules, in.ID)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			if err := em.AddStore(ctx, in.Body.URL); err != nil {
				return nil, huma.Error502BadGateway(err.Error())
			}
			return nil, nil
		})
	huma.Register(s.api, huma.Operation{OperationID: "stores-remove", Method: http.MethodDelete, Path: "/api/v1/modules/{id}/stores", Tags: etags},
		func(ctx context.Context, in *struct {
			ID  int64  `path:"id"`
			URL string `query:"url" minLength:"1"`
		}) (*struct{}, error) {
			em, _, err := modules.GetAs[source.ExtensionManager](s.app.Modules, in.ID)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			if err := em.RemoveStore(ctx, in.URL); err != nil {
				return nil, huma.Error502BadGateway(err.Error())
			}
			return nil, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "source-preferences", Method: http.MethodGet, Path: "/api/v1/sources/{moduleId}/{sourceId}/preferences", Tags: tags},
		func(ctx context.Context, in *struct {
			ModuleID int64  `path:"moduleId"`
			SourceID string `path:"sourceId"`
		}) (*struct{ Body []source.Preference }, error) {
			p, _, err := modules.GetAs[source.Preferences](s.app.Modules, in.ModuleID)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			list, err := p.SourcePreferences(ctx, in.SourceID)
			if err != nil {
				return nil, huma.Error502BadGateway(err.Error())
			}
			return &struct{ Body []source.Preference }{list}, nil
		})
	huma.Register(s.api, huma.Operation{OperationID: "source-preferences-set", Method: http.MethodPut, Path: "/api/v1/sources/{moduleId}/{sourceId}/preferences", Tags: tags},
		func(ctx context.Context, in *struct {
			ModuleID int64  `path:"moduleId"`
			SourceID string `path:"sourceId"`
			Body     struct {
				Position int    `json:"position"`
				Type     string `json:"type"`
				Value    any    `json:"value"`
			}
		}) (*struct{}, error) {
			p, _, err := modules.GetAs[source.Preferences](s.app.Modules, in.ModuleID)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			if err := p.SetSourcePreference(ctx, in.SourceID, in.Body.Position, in.Body.Type, in.Body.Value); err != nil {
				return nil, huma.Error502BadGateway(err.Error())
			}
			return nil, nil
		})
}

// cachedImage serves an image from <data>/cache/<bucket>, fetching when stale.
func (s *Server) cachedImage(ctx context.Context, bucket, key string, ttl time.Duration, fetch func(context.Context) (io.ReadCloser, string, error)) ([]byte, string, error) {
	sum := sha1.Sum([]byte(key))
	name := hex.EncodeToString(sum[:])
	dir := filepath.Join(s.app.Cfg.DataDir, "cache", bucket, name[:2])
	p := filepath.Join(dir, name)
	if st, err := os.Stat(p); err == nil && time.Since(st.ModTime()) < ttl {
		if data, err := os.ReadFile(p); err == nil {
			ct, _ := os.ReadFile(p + ".type")
			return data, string(ct), nil
		}
	}
	body, ct, err := fetch(ctx)
	if err != nil {
		return nil, "", err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, 20<<20))
	if err != nil {
		return nil, "", err
	}
	if ct == "" {
		ct = http.DetectContentType(data)
	}
	if err := os.MkdirAll(dir, 0o775); err == nil {
		_ = os.WriteFile(p, data, 0o664)
		_ = os.WriteFile(p+".type", []byte(ct), 0o664)
	}
	return data, ct, nil
}
