package api

import (
	"context"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/uptrace/bun"

	"github.com/Asion001/mangarr/internal/fsutil"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/settings"
)

type RootFolderResource struct {
	model.RootFolder
	Accessible bool   `json:"accessible"`
	FreeSpace  uint64 `json:"freeSpace"`
	Error      string `json:"error,omitempty"`
	SeriesCnt  int    `json:"seriesCount"`
}

// settingsDoc registers GET/PUT for one settings document.
func settingsDoc[T any](s *Server, name, key string, get func(context.Context) (T, error), after func(context.Context, T) error) {
	tags := []string{"Settings"}
	huma.Register(s.api, huma.Operation{OperationID: "settings-get-" + name, Method: http.MethodGet, Path: "/api/v1/settings/" + name, Tags: tags},
		func(ctx context.Context, _ *struct{}) (*struct{ Body T }, error) {
			v, err := get(ctx)
			return &struct{ Body T }{v}, toHTTPError(err)
		})
	huma.Register(s.api, huma.Operation{OperationID: "settings-put-" + name, Method: http.MethodPut, Path: "/api/v1/settings/" + name, Tags: tags},
		func(ctx context.Context, in *struct{ Body T }) (*struct{ Body T }, error) {
			if err := s.app.Settings.Set(ctx, key, in.Body); err != nil {
				return nil, toHTTPError(err)
			}
			// respond with the effective document (env-pinned fields win)
			v, err := get(ctx)
			if err != nil {
				return nil, toHTTPError(err)
			}
			if after != nil {
				if err := after(ctx, v); err != nil {
					return nil, toHTTPError(err)
				}
			}
			s.app.Bus.Changed("settings", "updated", 0)
			return &struct{ Body T }{v}, nil
		})
}

type GeneralSettingsResource struct {
	APIKey          string `json:"apiKey" readOnly:"true"`
	InstanceName    string `json:"instanceName"`
	PublicURL       string `json:"publicUrl"`
	BackupRetention int    `json:"backupRetention"`
}

func (s *Server) registerSettings() {
	tags := []string{"Settings"}

	// General settings hide the session secret and keep the API key read-only.
	huma.Register(s.api, huma.Operation{OperationID: "settings-get-general", Method: http.MethodGet, Path: "/api/v1/settings/general", Tags: tags},
		func(ctx context.Context, _ *struct{}) (*struct{ Body GeneralSettingsResource }, error) {
			g, err := s.app.Settings.General(ctx)
			return &struct{ Body GeneralSettingsResource }{GeneralSettingsResource{g.APIKey, g.InstanceName, g.PublicURL, g.BackupRetention}}, toHTTPError(err)
		})
	huma.Register(s.api, huma.Operation{OperationID: "settings-put-general", Method: http.MethodPut, Path: "/api/v1/settings/general", Tags: tags},
		func(ctx context.Context, in *struct{ Body GeneralSettingsResource }) (*struct{ Body GeneralSettingsResource }, error) {
			g, err := s.app.Settings.General(ctx)
			if err != nil {
				return nil, toHTTPError(err)
			}
			g.InstanceName, g.PublicURL, g.BackupRetention = in.Body.InstanceName, strings.TrimRight(in.Body.PublicURL, "/"), in.Body.BackupRetention
			if err := s.app.Settings.Set(ctx, settings.KeyGeneral, g); err != nil {
				return nil, toHTTPError(err)
			}
			return &struct{ Body GeneralSettingsResource }{GeneralSettingsResource{g.APIKey, g.InstanceName, g.PublicURL, g.BackupRetention}}, nil
		})
	huma.Register(s.api, huma.Operation{OperationID: "settings-regenerate-apikey", Method: http.MethodPost, Path: "/api/v1/settings/general/apikey", Tags: tags},
		func(ctx context.Context, _ *struct{}) (*struct{ Body GeneralSettingsResource }, error) {
			g, err := s.app.Settings.General(ctx)
			if err != nil {
				return nil, toHTTPError(err)
			}
			for _, l := range s.app.Settings.Locks(settings.KeyGeneral) {
				if l.Path == "apiKey" {
					return nil, huma.Error409Conflict("the API key is set by " + l.Env)
				}
			}
			g.APIKey = settings.RandomHex(16)
			if err := s.app.Settings.Set(ctx, settings.KeyGeneral, g); err != nil {
				return nil, toHTTPError(err)
			}
			return &struct{ Body GeneralSettingsResource }{GeneralSettingsResource{g.APIKey, g.InstanceName, g.PublicURL, g.BackupRetention}}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "settings-locks", Method: http.MethodGet, Path: "/api/v1/settings/locks", Tags: tags,
		Summary: "Settings fields pinned by environment variables, per document"},
		func(ctx context.Context, _ *struct{}) (*struct{ Body map[string][]settings.Lock }, error) {
			out := map[string][]settings.Lock{}
			for _, d := range settings.Docs {
				l := s.app.Settings.Locks(d.Key)
				if l == nil {
					l = []settings.Lock{}
				}
				out[d.Name] = l
			}
			return &struct{ Body map[string][]settings.Lock }{out}, nil
		})

	settingsDoc(s, "media", settings.KeyMediaManagement, s.app.Settings.MediaManagement, nil)
	settingsDoc(s, "downloads", settings.KeyDownloads, s.app.Settings.Downloads, nil)
	settingsDoc(s, "cleanup", settings.KeyCleanup, s.app.Settings.Cleanup, nil)
	settingsDoc(s, "readsync", settings.KeyReadSync, s.app.Settings.ReadSync, func(ctx context.Context, v settings.ReadSync) error {
		if v.IntervalMinutes < 5 {
			v.IntervalMinutes = 5
		}
		return s.app.Scheduler.SetInterval(ctx, "SyncReadProgress", time.Duration(v.IntervalMinutes)*time.Minute)
	})

	// ---- root folders
	rtags := []string{"Root folders"}
	huma.Register(s.api, huma.Operation{OperationID: "rootfolders-list", Method: http.MethodGet, Path: "/api/v1/rootfolders", Tags: rtags},
		func(ctx context.Context, _ *struct{}) (*struct{ Body []RootFolderResource }, error) {
			var rows []model.RootFolder
			if err := s.app.DB.NewSelect().Model(&rows).Order("path").Scan(ctx); err != nil {
				return nil, toHTTPError(err)
			}
			out := make([]RootFolderResource, 0, len(rows))
			for _, r := range rows {
				res := RootFolderResource{RootFolder: r}
				if err := fsutil.Writable(r.Path); err != nil {
					res.Error = err.Error()
				} else {
					res.Accessible = true
					res.FreeSpace, _ = fsutil.FreeSpace(r.Path)
				}
				n, _ := s.app.DB.NewSelect().Model((*model.Series)(nil)).Where("root_folder_id = ?", r.ID).Count(ctx)
				res.SeriesCnt = n
				out = append(out, res)
			}
			return &struct{ Body []RootFolderResource }{out}, nil
		})
	huma.Register(s.api, huma.Operation{OperationID: "rootfolders-create", Method: http.MethodPost, Path: "/api/v1/rootfolders", Tags: rtags},
		func(ctx context.Context, in *struct {
			Body struct {
				Path     string `json:"path" minLength:"1"`
				Language string `json:"language"`
			}
		}) (*struct{ Body model.RootFolder }, error) {
			p := filepath.Clean(in.Body.Path)
			if !filepath.IsAbs(p) {
				return nil, huma.Error400BadRequest("path must be absolute")
			}
			if err := fsutil.Writable(p); err != nil {
				return nil, huma.Error400BadRequest("folder is not writable: " + err.Error())
			}
			rf := model.RootFolder{Path: p, Language: in.Body.Language, CreatedAt: time.Now().UTC()}
			if _, err := s.app.DB.NewInsert().Model(&rf).Exec(ctx); err != nil {
				return nil, huma.Error409Conflict("root folder already exists")
			}
			s.app.Bus.Changed("rootfolder", "created", rf.ID)
			return &struct{ Body model.RootFolder }{rf}, nil
		})
	huma.Register(s.api, huma.Operation{OperationID: "rootfolders-delete", Method: http.MethodDelete, Path: "/api/v1/rootfolders/{id}", Tags: rtags},
		func(ctx context.Context, in *IDPath) (*struct{}, error) {
			var rf model.RootFolder
			if err := s.app.DB.NewSelect().Model(&rf).Where("id = ?", in.ID).Scan(ctx); err == nil && rf.ManagedBy != "" {
				return nil, huma.Error409Conflict("this root folder is set by MANGARR_ROOT_FOLDERS")
			}
			n, _ := s.app.DB.NewSelect().Model((*model.Series)(nil)).Where("root_folder_id = ?", in.ID).Count(ctx)
			if n > 0 {
				return nil, huma.Error409Conflict("root folder still has series")
			}
			_, err := s.app.DB.NewDelete().Model((*model.RootFolder)(nil)).Where("id = ?", in.ID).Exec(ctx)
			s.app.Bus.Changed("rootfolder", "deleted", in.ID)
			return nil, toHTTPError(err)
		})

	// ---- tags
	ttags := []string{"Tags"}
	huma.Register(s.api, huma.Operation{OperationID: "tags-list", Method: http.MethodGet, Path: "/api/v1/tags", Tags: ttags},
		func(ctx context.Context, _ *struct{}) (*struct{ Body []model.Tag }, error) {
			var rows []model.Tag
			err := s.app.DB.NewSelect().Model(&rows).Order("label").Scan(ctx)
			if rows == nil {
				rows = []model.Tag{}
			}
			return &struct{ Body []model.Tag }{rows}, toHTTPError(err)
		})
	huma.Register(s.api, huma.Operation{OperationID: "tags-create", Method: http.MethodPost, Path: "/api/v1/tags", Tags: ttags},
		func(ctx context.Context, in *struct {
			Body struct {
				Label string `json:"label" minLength:"1"`
			}
		}) (*struct{ Body model.Tag }, error) {
			t := model.Tag{Label: strings.ToLower(strings.TrimSpace(in.Body.Label))}
			var existing model.Tag
			if err := s.app.DB.NewSelect().Model(&existing).Where("label = ?", t.Label).Scan(ctx); err == nil {
				return &struct{ Body model.Tag }{existing}, nil
			}
			_, err := s.app.DB.NewInsert().Model(&t).Exec(ctx)
			s.app.Bus.Changed("tag", "created", t.ID)
			return &struct{ Body model.Tag }{t}, toHTTPError(err)
		})
	huma.Register(s.api, huma.Operation{OperationID: "tags-delete", Method: http.MethodDelete, Path: "/api/v1/tags/{id}", Tags: ttags},
		func(ctx context.Context, in *IDPath) (*struct{}, error) {
			_, err := s.app.DB.NewDelete().Model((*model.Tag)(nil)).Where("id = ?", in.ID).Exec(ctx)
			s.app.Bus.Changed("tag", "deleted", in.ID)
			return nil, toHTTPError(err)
		})

	// ---- profiles
	ptags := []string{"Profiles"}
	huma.Register(s.api, huma.Operation{OperationID: "profiles-list", Method: http.MethodGet, Path: "/api/v1/profiles", Tags: ptags},
		func(ctx context.Context, _ *struct{}) (*struct{ Body []model.Profile }, error) {
			var rows []model.Profile
			err := s.app.DB.NewSelect().Model(&rows).Order("id").Scan(ctx)
			return &struct{ Body []model.Profile }{rows}, toHTTPError(err)
		})
	huma.Register(s.api, huma.Operation{OperationID: "profiles-create", Method: http.MethodPost, Path: "/api/v1/profiles", Tags: ptags},
		func(ctx context.Context, in *struct{ Body model.Profile }) (*struct{ Body model.Profile }, error) {
			p := in.Body
			p.ID = 0
			now := time.Now().UTC()
			p.CreatedAt, p.UpdatedAt = now, now
			if err := validateProfile(&p); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			err := s.app.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
				if _, err := tx.NewInsert().Model(&p).Exec(ctx); err != nil {
					return err
				}
				return clearOtherDefaults(ctx, tx, &p)
			})
			if err != nil {
				return nil, huma.Error409Conflict(err.Error())
			}
			s.app.Bus.Changed("profile", "created", p.ID)
			return &struct{ Body model.Profile }{p}, nil
		})
	huma.Register(s.api, huma.Operation{OperationID: "profiles-update", Method: http.MethodPut, Path: "/api/v1/profiles/{id}", Tags: ptags},
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body model.Profile
		}) (*struct{ Body model.Profile }, error) {
			var stored model.Profile
			if err := s.app.DB.NewSelect().Model(&stored).Where("id = ?", in.ID).Scan(ctx); err != nil {
				return nil, huma.Error404NotFound("profile not found")
			}
			p := in.Body
			p.ID, p.CreatedAt, p.UpdatedAt = in.ID, stored.CreatedAt, time.Now().UTC()
			if err := validateProfile(&p); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			if stored.IsDefault && !p.IsDefault {
				return nil, huma.Error400BadRequest("mark another profile as default instead")
			}
			err := s.app.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
				if _, err := tx.NewUpdate().Model(&p).WherePK().Exec(ctx); err != nil {
					return err
				}
				return clearOtherDefaults(ctx, tx, &p)
			})
			if err != nil {
				return nil, toHTTPError(err)
			}
			s.app.Bus.Changed("profile", "updated", p.ID)
			return &struct{ Body model.Profile }{p}, nil
		})
	huma.Register(s.api, huma.Operation{OperationID: "profiles-delete", Method: http.MethodDelete, Path: "/api/v1/profiles/{id}", Tags: ptags},
		func(ctx context.Context, in *IDPath) (*struct{}, error) {
			n, _ := s.app.DB.NewSelect().Model((*model.Series)(nil)).Where("profile_id = ?", in.ID).Count(ctx)
			if n > 0 {
				return nil, huma.Error409Conflict("profile is used by series")
			}
			var p model.Profile
			if err := s.app.DB.NewSelect().Model(&p).Where("id = ?", in.ID).Scan(ctx); err == nil && p.IsDefault {
				return nil, huma.Error409Conflict("cannot delete the default profile")
			}
			_, err := s.app.DB.NewDelete().Model((*model.Profile)(nil)).Where("id = ?", in.ID).Exec(ctx)
			s.app.Bus.Changed("profile", "deleted", in.ID)
			return nil, toHTTPError(err)
		})
}

// clearOtherDefaults keeps exactly one default profile.
func clearOtherDefaults(ctx context.Context, tx bun.Tx, p *model.Profile) error {
	if !p.IsDefault {
		return nil
	}
	_, err := tx.NewUpdate().Model((*model.Profile)(nil)).Set("is_default = ?", false).Where("id <> ?", p.ID).Exec(ctx)
	return err
}

func validateProfile(p *model.Profile) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return badRequest("name is required")
	}
	if p.Config.PreferredScanlators == nil {
		p.Config.PreferredScanlators = []string{}
	}
	if p.Config.BlockedScanlators == nil {
		p.Config.BlockedScanlators = []string{}
	}
	for _, re := range append(append([]string{}, p.Config.PreferredScanlators...), p.Config.BlockedScanlators...) {
		if _, err := compileScanlatorPattern(re); err != nil {
			return badRequest("invalid scanlator pattern " + re + ": " + err.Error())
		}
	}
	u := &p.Config.Upscale
	if u.MinWidth <= 0 {
		u.MinWidth = 1400
	}
	if u.Format == "" {
		u.Format = "webp"
	}
	if u.Quality <= 0 || u.Quality > 100 {
		u.Quality = 90
	}
	return nil
}

func compileScanlatorPattern(p string) (*regexp.Regexp, error) { return regexp.Compile("(?i)" + p) }
