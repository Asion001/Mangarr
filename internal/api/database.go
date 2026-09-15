package api

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/app"
	"github.com/Asion001/mangarr/internal/config"
	"github.com/Asion001/mangarr/internal/db"
)

func init() { register((*Server).registerDatabase) }

// DatabaseInfo describes the database mangarr uses and a move in progress.
type DatabaseInfo struct {
	Kind string `json:"kind" enum:"sqlite,postgres"`
	// DSN is the address with the password hidden.
	DSN string `json:"dsn"`
	// Source: env (MANGARR_DB pins it), file (moved from this page) or default.
	Source string `json:"source" enum:"env,file,default"`
	// SizeBytes is the SQLite file's size (0 for Postgres).
	SizeBytes int64 `json:"sizeBytes"`
	// DefaultDSN is the SQLite file in the data dir (to move back to).
	DefaultDSN string        `json:"defaultDsn"`
	Move       app.MoveState `json:"move"`
}

type dsnBody struct {
	Body struct {
		DSN string `json:"dsn" minLength:"1" doc:"postgres://user:password@host:5432/database?sslmode=disable, or sqlite:///path/file.db"`
		// Overwrite empties a target that already has mangarr data.
		Overwrite bool `json:"overwrite,omitempty"`
	}
}

func (s *Server) registerDatabase() {
	tags := []string{"System"}
	huma.Register(s.api, huma.Operation{OperationID: "database-get", Method: http.MethodGet, Path: "/api/v1/system/database", Tags: tags,
		Summary: "The database mangarr uses, and the progress of moving it"},
		func(ctx context.Context, _ *struct{}) (*struct{ Body DatabaseInfo }, error) {
			info := DatabaseInfo{Kind: string(s.app.DB.Kind), DSN: app.RedactDSN(s.app.Cfg.DB), Source: s.app.Cfg.DBSource,
				DefaultDSN: config.DefaultDB(s.app.Cfg.DataDir), Move: s.app.MoveState()}
			if s.app.DB.Kind == db.SQLite {
				for _, suffix := range []string{"", "-wal"} {
					if st, err := os.Stat(s.app.DB.Path + suffix); err == nil {
						info.SizeBytes += st.Size()
					}
				}
			}
			return &struct{ Body DatabaseInfo }{info}, nil
		})
	huma.Register(s.api, huma.Operation{OperationID: "database-test", Method: http.MethodPost, Path: "/api/v1/system/database/test", Tags: tags,
		Summary: "Connect to a database the data could move to"},
		func(ctx context.Context, in *dsnBody) (*struct{ Body app.DBTest }, error) {
			t, err := s.app.TestDatabase(ctx, strings.TrimSpace(in.Body.DSN))
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return &struct{ Body app.DBTest }{*t}, nil
		})
	huma.Register(s.api, huma.Operation{OperationID: "database-move", Method: http.MethodPost, Path: "/api/v1/system/database/move", Tags: tags,
		Summary: "Copy all data to another database and switch to it (mangarr restarts)"},
		func(ctx context.Context, in *dsnBody) (*struct{}, error) {
			if err := s.app.MoveDatabase(strings.TrimSpace(in.Body.DSN), in.Body.Overwrite); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return nil, nil
		})
	huma.Register(s.api, huma.Operation{OperationID: "database-move-cancel", Method: http.MethodPost, Path: "/api/v1/system/database/cancel", Tags: tags,
		Summary: "Leave maintenance mode without switching (after a failed move, or when MANGARR_DB can't be changed now)"},
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			if s.app.MoveState().Running {
				return nil, huma.Error409Conflict("the database is still being copied")
			}
			s.app.EndMaintenance()
			return nil, nil
		})
	huma.Register(s.api, huma.Operation{OperationID: "system-restart", Method: http.MethodPost, Path: "/api/v1/system/restart", Tags: tags,
		Summary: "Restart mangarr"},
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			s.app.RequestRestart()
			return nil, nil
		})
}

// maintenanceAllowed are the writes allowed while the database moves.
var maintenanceAllowed = map[string]bool{
	"/api/v1/system/database/cancel": true,
	"/api/v1/system/restart":         true,
	"/api/v1/auth/login":             true,
	"/api/v1/auth/logout":            true,
}

// maintenanceMiddleware answers writes with 503 while the database moves.
func (s *Server) maintenanceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.app.InMaintenance() && r.Method != http.MethodGet && r.Method != http.MethodHead && !maintenanceAllowed[r.URL.Path] {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "10")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"title":"Service Unavailable","status":503,"detail":"mangarr is moving its database; try again in a moment"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
