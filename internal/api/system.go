package api

import (
	"context"
	"net/http"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/diskcache"
	"github.com/Asion001/mangarr/internal/envcfg"
	"github.com/Asion001/mangarr/internal/logging"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/version"
)

type SystemStatus struct {
	Version   string    `json:"version"`
	Commit    string    `json:"commit"`
	GoVersion string    `json:"goVersion"`
	OS        string    `json:"os"`
	Arch      string    `json:"arch"`
	Database  string    `json:"database"`
	DataDir   string    `json:"dataDir"`
	StartedAt time.Time `json:"startedAt"`
	URLBase   string    `json:"urlBase"`
}

type CacheStatus struct {
	// Entries/Bytes of the in-memory catalog cache (search results, details).
	Entries  int                     `json:"entries"`
	Bytes    int64                   `json:"bytes"`
	MaxBytes int64                   `json:"maxBytes"`
	Images   []diskcache.BucketStats `json:"images"`
}

type TaskInfo struct {
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	IntervalMinutes int        `json:"intervalMinutes"`
	LastExecution   *time.Time `json:"lastExecution,omitempty"`
	NextExecution   *time.Time `json:"nextExecution,omitempty"`
	Scheduled       bool       `json:"scheduled"`
}

type CommandInput struct {
	Name string         `json:"name"`
	Body map[string]any `json:"body,omitempty"`
}

func (s *Server) registerSystem() {
	tags := []string{"System"}
	huma.Register(s.api, huma.Operation{OperationID: "system-status", Method: http.MethodGet, Path: "/api/v1/system/status", Tags: tags},
		func(ctx context.Context, _ *struct{}) (*struct{ Body SystemStatus }, error) {
			return &struct{ Body SystemStatus }{SystemStatus{
				Version: version.Version, Commit: version.Commit, GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH,
				Database: string(s.app.DB.Kind), DataDir: s.app.Cfg.DataDir, StartedAt: s.app.StartedAt, URLBase: s.app.Cfg.URLBase,
			}}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "system-env", Method: http.MethodGet, Path: "/api/v1/system/env", Tags: tags,
		Summary: "Supported environment variables and which are set"},
		func(ctx context.Context, _ *struct{}) (*struct {
			Body struct {
				Vars    []envcfg.Var `json:"vars"`
				Unknown []string     `json:"unknown"`
			}
		}, error) {
			out := &struct {
				Body struct {
					Vars    []envcfg.Var `json:"vars"`
					Unknown []string     `json:"unknown"`
				}
			}{}
			out.Body.Vars = envcfg.All(s.app.Cfg.Env)
			out.Body.Unknown = envcfg.Unknown(s.app.Cfg.Env)
			if out.Body.Unknown == nil {
				out.Body.Unknown = []string{}
			}
			return out, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "system-cache", Method: http.MethodGet, Path: "/api/v1/system/cache", Tags: tags,
		Summary: "Sizes of the in-memory catalog cache and the on-disk image cache"},
		func(ctx context.Context, _ *struct{}) (*struct{ Body CacheStatus }, error) {
			n, b, max := s.app.SourceCache.Stats()
			return &struct{ Body CacheStatus }{CacheStatus{Entries: n, Bytes: b, MaxBytes: max,
				Images: diskcache.Stats(filepath.Join(s.app.Cfg.DataDir, "cache"))}}, nil
		})
	huma.Register(s.api, huma.Operation{OperationID: "system-cache-clear", Method: http.MethodPost, Path: "/api/v1/system/cache/clear", Tags: tags,
		Summary: "Clear caches: catalogs (search/details) and image buckets"},
		func(ctx context.Context, in *struct {
			Body struct {
				Catalogs bool     `json:"catalogs"`
				Images   []string `json:"images,omitempty" doc:"Image buckets to clear (thumbs, assets, covers); empty = none"`
			}
		}) (*struct{ Body CacheStatus }, error) {
			if in.Body.Catalogs {
				s.app.SourceCache.Clear()
				s.app.Catalogs.Invalidate(0)
			}
			for _, b := range in.Body.Images {
				if !slices.Contains(diskcache.Buckets, b) {
					return nil, huma.Error400BadRequest("unknown image bucket " + b)
				}
			}
			if len(in.Body.Images) > 0 {
				if err := diskcache.Clear(filepath.Join(s.app.Cfg.DataDir, "cache"), in.Body.Images); err != nil {
					return nil, toHTTPError(err)
				}
			}
			s.app.Bus.Changed("cache", "cleared", 0)
			n, b, max := s.app.SourceCache.Stats()
			return &struct{ Body CacheStatus }{CacheStatus{Entries: n, Bytes: b, MaxBytes: max,
				Images: diskcache.Stats(filepath.Join(s.app.Cfg.DataDir, "cache"))}}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "system-logs", Method: http.MethodGet, Path: "/api/v1/system/logs", Tags: tags},
		func(ctx context.Context, in *struct {
			Level string `query:"level" default:"info" enum:"debug,info,warn,error"`
			Limit int    `query:"limit" default:"500" minimum:"1" maximum:"2000"`
		}) (*struct{ Body []logging.Entry }, error) {
			return &struct{ Body []logging.Entry }{s.app.LogRing.Entries(logging.ParseLevel(in.Level), in.Limit)}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "system-tasks", Method: http.MethodGet, Path: "/api/v1/system/tasks", Tags: tags},
		func(ctx context.Context, _ *struct{}) (*struct{ Body []TaskInfo }, error) {
			rows, err := s.app.Scheduler.Tasks(ctx)
			if err != nil {
				return nil, toHTTPError(err)
			}
			byName := map[string]model.ScheduledTask{}
			for _, r := range rows {
				byName[r.Name] = r
			}
			var out []TaskInfo
			for _, d := range s.app.Queue.Definitions() {
				ti := TaskInfo{Name: d.Name, Description: d.Description}
				if r, ok := byName[d.Name]; ok {
					ti.Scheduled = true
					ti.IntervalMinutes = r.IntervalMinutes
					ti.LastExecution = r.LastExecution
					if r.IntervalMinutes > 0 {
						next := time.Now().UTC()
						if r.LastExecution != nil {
							next = r.LastExecution.Add(time.Duration(r.IntervalMinutes) * time.Minute)
						}
						ti.NextExecution = &next
					}
				}
				out = append(out, ti)
			}
			return &struct{ Body []TaskInfo }{out}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "commands-list", Method: http.MethodGet, Path: "/api/v1/commands", Tags: tags},
		func(ctx context.Context, in *struct {
			Limit int `query:"limit" default:"50" maximum:"500"`
		}) (*struct{ Body []model.Command }, error) {
			out, err := s.app.Queue.Recent(ctx, in.Limit)
			if out == nil {
				out = []model.Command{}
			}
			// overlay live messages of running commands
			live := map[int64]*model.Command{}
			for _, c := range s.app.Queue.Active() {
				live[c.ID] = c
			}
			for i := range out {
				if c, ok := live[out[i].ID]; ok {
					out[i] = *c
				}
			}
			return &struct{ Body []model.Command }{out}, toHTTPError(err)
		})

	huma.Register(s.api, huma.Operation{OperationID: "commands-push", Method: http.MethodPost, Path: "/api/v1/commands", Tags: tags,
		Summary: "Queue a command (e.g. RefreshSources, RefreshSeries {seriesId}, SearchMissing, Cleanup)"},
		func(ctx context.Context, in *struct{ Body CommandInput }) (*struct{ Body *model.Command }, error) {
			c, err := s.app.Queue.Push(ctx, in.Body.Name, in.Body.Body, "manual")
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return &struct{ Body *model.Command }{c}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "commands-get", Method: http.MethodGet, Path: "/api/v1/commands/{id}", Tags: tags},
		func(ctx context.Context, in *IDPath) (*struct{ Body *model.Command }, error) {
			c, err := s.app.Queue.Get(ctx, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound("command not found")
			}
			return &struct{ Body *model.Command }{c}, nil
		})
}
