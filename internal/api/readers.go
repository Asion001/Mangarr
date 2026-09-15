package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/cleanup"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/library"
	"github.com/Asion001/mangarr/internal/reading"
	"github.com/Asion001/mangarr/internal/readsync"
)

func init() {
	register((*Server).registerReaders)
	accountFieldsOf = func(impl *modules.Implementation) []modules.Field {
		if impl.Kind != modules.KindLibrary {
			return nil
		}
		// build a throwaway instance with defaults to ask for its account fields
		inst, err := impl.New(modules.Deps{}, impl.Settings())
		if err != nil {
			return nil
		}
		if pr, ok := inst.(library.ProgressReader); ok {
			return pr.AccountFields()
		}
		return nil
	}
}

type ReaderAccountView struct {
	model.ReaderAccount
	ModuleName string `json:"moduleName"`
	// LiveCapable is true when the server pushes progress changes (Komga);
	// Live is the connection's state then.
	LiveCapable bool                  `json:"liveCapable"`
	Live        *readsync.WatchStatus `json:"live,omitempty"`
}

type ReaderResource struct {
	model.Reader
	Accounts []ReaderAccountView `json:"accounts"`
	// Chapters this reader finished (across all series).
	CompletedCount int `json:"completedCount"`
}

// ReaderSync is a reader's sync health: every app, device and server that
// reported progress, and the latest reports.
type ReaderSync struct {
	reading.SyncHealth
	// ReadingApps is true when reading apps (the Komga-compatible API) act
	// as this reader; Keys are their devices then.
	ReadingApps bool               `json:"readingApps"`
	Keys        []model.ReadingKey `json:"keys"`
}

type AccountInput struct {
	ModuleID    int64             `json:"moduleId"`
	Credentials map[string]string `json:"credentials"`
}

func (s *Server) readerResources(ctx context.Context) ([]ReaderResource, error) {
	var readers []model.Reader
	if err := s.app.DB.NewSelect().Model(&readers).Order("name").Scan(ctx); err != nil {
		return nil, err
	}
	var accounts []model.ReaderAccount
	if err := s.app.DB.NewSelect().Model(&accounts).Scan(ctx); err != nil {
		return nil, err
	}
	var counts []struct {
		ReaderID int64 `bun:"reader_id"`
		N        int   `bun:"n"`
	}
	_ = s.app.DB.NewSelect().TableExpr("chapter_read_states").ColumnExpr("reader_id, COUNT(*) AS n").
		Where("completed = ?", true).GroupExpr("reader_id").Scan(ctx, &counts)
	countBy := map[int64]int{}
	for _, c := range counts {
		countBy[c.ReaderID] = c.N
	}
	live := s.app.Watcher.Status()
	out := make([]ReaderResource, 0, len(readers))
	for _, r := range readers {
		rr := ReaderResource{Reader: r, Accounts: []ReaderAccountView{}, CompletedCount: countBy[r.ID]}
		for _, a := range accounts {
			if a.ReaderID != r.ID {
				continue
			}
			name := ""
			if l, ok := s.app.Modules.Get(a.ModuleID); ok {
				name = l.Def.Name
			}
			v := ReaderAccountView{ReaderAccount: a, ModuleName: name}
			if _, _, err := modules.GetAs[library.ProgressWatcher](s.app.Modules, a.ModuleID); err == nil {
				v.LiveCapable = true
				if st, ok := live[a.ID]; ok {
					v.Live = &st
				}
			}
			rr.Accounts = append(rr.Accounts, v)
		}
		out = append(out, rr)
	}
	return out, nil
}

func (s *Server) registerReaders() {
	tags := []string{"Readers"}
	huma.Register(s.api, huma.Operation{OperationID: "readers-list", Method: http.MethodGet, Path: "/api/v1/readers", Tags: tags},
		func(ctx context.Context, _ *struct{}) (*struct{ Body []ReaderResource }, error) {
			out, err := s.readerResources(ctx)
			return &struct{ Body []ReaderResource }{out}, toHTTPError(err)
		})
	huma.Register(s.api, huma.Operation{OperationID: "readers-create", Method: http.MethodPost, Path: "/api/v1/readers", Tags: tags},
		func(ctx context.Context, in *struct {
			Body struct {
				Name            string `json:"name" minLength:"1"`
				CountForCleanup bool   `json:"countForCleanup"`
			}
		}) (*struct{ Body model.Reader }, error) {
			r := model.Reader{Name: strings.TrimSpace(in.Body.Name), CountForCleanup: in.Body.CountForCleanup, CreatedAt: time.Now().UTC()}
			if _, err := s.app.DB.NewInsert().Model(&r).Exec(ctx); err != nil {
				return nil, huma.Error409Conflict("reader already exists")
			}
			s.app.Bus.Changed("readers", "created", r.ID)
			return &struct{ Body model.Reader }{r}, nil
		})
	huma.Register(s.api, huma.Operation{OperationID: "readers-update", Method: http.MethodPut, Path: "/api/v1/readers/{id}", Tags: tags},
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body struct {
				Name            string `json:"name" minLength:"1"`
				CountForCleanup bool   `json:"countForCleanup"`
			}
		}) (*struct{}, error) {
			_, err := s.app.DB.NewUpdate().Model((*model.Reader)(nil)).Set("name = ?", strings.TrimSpace(in.Body.Name)).
				Set("count_for_cleanup = ?", in.Body.CountForCleanup).Where("id = ?", in.ID).Exec(ctx)
			s.app.Bus.Changed("readers", "updated", in.ID)
			return nil, toHTTPError(err)
		})
	huma.Register(s.api, huma.Operation{OperationID: "readers-delete", Method: http.MethodDelete, Path: "/api/v1/readers/{id}", Tags: tags},
		func(ctx context.Context, in *IDPath) (*struct{}, error) {
			_, err := s.app.DB.NewDelete().Model((*model.Reader)(nil)).Where("id = ?", in.ID).Exec(ctx)
			s.app.Bus.Changed("readers", "deleted", in.ID)
			return nil, toHTTPError(err)
		})
	huma.Register(s.api, huma.Operation{OperationID: "readers-sync", Method: http.MethodGet, Path: "/api/v1/readers/{id}/sync", Tags: tags,
		Summary: "Sync health: apps, devices and servers that reported the reader's progress, and recent reports"},
		func(ctx context.Context, in *struct {
			ID    int64 `path:"id"`
			Limit int   `query:"limit" default:"50" minimum:"1" maximum:"500"`
		}) (*struct{ Body ReaderSync }, error) {
			h, err := s.app.Reading.SyncHealth(ctx, in.ID, in.Limit)
			if err != nil {
				return nil, toHTTPError(err)
			}
			out := ReaderSync{SyncHealth: h, Keys: []model.ReadingKey{}}
			if rid, err := s.app.Reading.ReaderID(ctx); err == nil && rid == in.ID {
				out.ReadingApps = true
				if err := s.app.DB.NewSelect().Model(&out.Keys).Order("created_at").Scan(ctx); err != nil {
					return nil, toHTTPError(err)
				}
			}
			return &struct{ Body ReaderSync }{out}, nil
		})
	huma.Register(s.api, huma.Operation{OperationID: "readers-account-save", Method: http.MethodPost, Path: "/api/v1/readers/{id}/accounts", Tags: tags,
		Summary: "Add or replace the reader's account on a library module (credentials are tested first)"},
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body AccountInput
		}) (*struct{ Body model.ReaderAccount }, error) {
			user, err := s.app.ReadSync.TestAccount(ctx, in.Body.ModuleID, in.Body.Credentials)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			acc := model.ReaderAccount{ReaderID: in.ID, ModuleID: in.Body.ModuleID, Credentials: in.Body.Credentials, ExternalUser: user, CreatedAt: time.Now().UTC()}
			_, err = s.app.DB.NewInsert().Model(&acc).
				On("CONFLICT (reader_id, module_id) DO UPDATE").
				Set("credentials = EXCLUDED.credentials").Set("external_user = EXCLUDED.external_user").Set("last_error = ''").
				Exec(ctx)
			if err != nil {
				return nil, toHTTPError(err)
			}
			s.app.Bus.Changed("readers", "updated", in.ID)
			_, _ = s.app.Queue.Push(ctx, "SyncReadProgress", nil, "account-added")
			go s.app.Watcher.Refresh(context.Background())
			return &struct{ Body model.ReaderAccount }{acc}, nil
		})
	huma.Register(s.api, huma.Operation{OperationID: "readers-account-delete", Method: http.MethodDelete, Path: "/api/v1/readers/{id}/accounts/{accountId}", Tags: tags},
		func(ctx context.Context, in *struct {
			ID        int64 `path:"id"`
			AccountID int64 `path:"accountId"`
		}) (*struct{}, error) {
			_, err := s.app.DB.NewDelete().Model((*model.ReaderAccount)(nil)).Where("id = ? AND reader_id = ?", in.AccountID, in.ID).Exec(ctx)
			s.app.Bus.Changed("readers", "updated", in.ID)
			go s.app.Watcher.Refresh(context.Background())
			return nil, toHTTPError(err)
		})

	ctags := []string{"Cleanup"}
	huma.Register(s.api, huma.Operation{OperationID: "cleanup-preview", Method: http.MethodGet, Path: "/api/v1/cleanup/preview", Tags: ctags,
		Summary: "What a cleanup run would delete with the current settings"},
		func(ctx context.Context, _ *struct{}) (*struct{ Body *cleanup.Plan }, error) {
			p, err := s.app.Cleaner.Plan(ctx)
			return &struct{ Body *cleanup.Plan }{p}, toHTTPError(err)
		})
	huma.Register(s.api, huma.Operation{OperationID: "cleanup-run", Method: http.MethodPost, Path: "/api/v1/cleanup/run", Tags: ctags,
		Summary: "Queue a cleanup. force=true deletes even when dry run is enabled."},
		func(ctx context.Context, in *struct {
			Force bool `query:"force"`
		}) (*struct{ Body *model.Command }, error) {
			c, err := s.app.Queue.Push(ctx, "Cleanup", map[string]any{"force": in.Force}, "manual")
			return &struct{ Body *model.Command }{c}, toHTTPError(err)
		})
	huma.Register(s.api, huma.Operation{OperationID: "chapter-restore", Method: http.MethodPost, Path: "/api/v1/chapters/{id}/restore", Tags: ctags,
		Summary: "Make a cleaned chapter wanted again and download it"},
		func(ctx context.Context, in *IDPath) (*struct{ Body *model.Chapter }, error) {
			ch, err := s.app.Cleaner.Restore(ctx, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound(err.Error())
			}
			_, _ = s.app.Queue.Push(ctx, "SearchMissing", map[string]any{"seriesId": ch.SeriesID, "chapterIds": []int64{ch.ID}, "explicit": true}, "restore")
			return &struct{ Body *model.Chapter }{ch}, nil
		})
}
