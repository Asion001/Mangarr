// Package komga implements the Komga library module: library rescans after
// imports and per-user read progress (for read-based cleanup).
package komga

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/httpx"
	"github.com/Asion001/mangarr/internal/modules/library"
)

type Settings struct {
	URL          string            `json:"url" label:"Komga URL" type:"url" required:"true" placeholder:"http://komga:25600" order:"1"`
	APIKey       string            `json:"apiKey" label:"Admin API key" secret:"true" required:"true" order:"2" help:"API key of a Komga admin (Account settings → API keys). Needed to trigger scans."`
	PathMappings map[string]string `json:"pathMappings" label:"Path mappings" type:"keyvalue" order:"3" help:"mangarr path → Komga path, when Komga mounts the library at a different path."`
	DeepScan     bool              `json:"deepScan" label:"Deep scan" order:"4" advanced:"true" help:"Ignore modification times (slower; use on network shares)."`
}

func init() {
	modules.Register(&modules.Implementation{
		Kind: modules.KindLibrary, Name: "komga", DisplayName: "Komga",
		Description: "Tells Komga to rescan after imports and reads per-user progress for read-based cleanup.",
		InfoURL:     "https://komga.org",
		Settings:    func() any { return &Settings{PathMappings: map[string]string{}} },
		New: func(deps modules.Deps, s any) (modules.Instance, error) {
			st := s.(*Settings)
			hc := deps.HTTP
			if hc == nil {
				hc = &http.Client{Timeout: time.Minute}
			}
			return &Module{s: st, http: hc, pm: library.NewPathMap(st.PathMappings)}, nil
		},
	})
}

type Module struct {
	s    *Settings
	http *http.Client
	pm   library.PathMap
}

func (m *Module) do(ctx context.Context, key, method, p string, body, out any) error {
	return httpx.Do(ctx, m.http, method, httpx.Join(m.s.URL, p), map[string]string{"X-API-Key": key}, body, out)
}

type user struct {
	ID    string   `json:"id"`
	Email string   `json:"email"`
	Roles []string `json:"roles"`
}

func (m *Module) me(ctx context.Context, key string) (*user, error) {
	var u user
	if err := m.do(ctx, key, http.MethodGet, "/api/v2/users/me", nil, &u); err != nil {
		var se *httpx.StatusError
		if errors.As(err, &se) && se.Code == 401 {
			return nil, errors.New("invalid Komga API key")
		}
		return nil, err
	}
	return &u, nil
}

func (m *Module) Test(ctx context.Context) error {
	u, err := m.me(ctx, m.s.APIKey)
	if err != nil {
		return err
	}
	for _, r := range u.Roles {
		if r == "ADMIN" {
			return nil
		}
	}
	return fmt.Errorf("Komga user %s is not an admin; scans require an admin API key", u.Email)
}

type komgaLibrary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Root string `json:"root"`
}

func (m *Module) libraries(ctx context.Context) ([]komgaLibrary, error) {
	var libs []komgaLibrary
	err := m.do(ctx, m.s.APIKey, http.MethodGet, "/api/v1/libraries", nil, &libs)
	return libs, err
}

func (m *Module) Rescan(ctx context.Context, localPaths []string) error {
	libs, err := m.libraries(ctx)
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	var unmatched []string
	for _, lp := range localPaths {
		remote := m.pm.ToRemote(lp)
		found := false
		for _, l := range libs {
			if library.Under(remote, l.Root) {
				ids[l.ID] = true
				found = true
			}
		}
		if !found {
			unmatched = append(unmatched, remote)
		}
	}
	for id := range ids {
		q := "/api/v1/libraries/" + id + "/scan"
		if m.s.DeepScan {
			q += "?deep=true"
		}
		if err := m.do(ctx, m.s.APIKey, http.MethodPost, q, nil, nil); err != nil {
			return fmt.Errorf("scan library %s: %w", id, err)
		}
	}
	if len(unmatched) > 0 && len(ids) == 0 {
		return fmt.Errorf("no Komga library contains %s (check path mappings)", strings.Join(unmatched, ", "))
	}
	return nil
}

// ---- progress --------------------------------------------------------------------

func (m *Module) AccountFields() []modules.Field {
	return []modules.Field{{Name: "apiKey", Label: "Komga API key", Type: "password", Secret: true, Required: true,
		Help: "The reader's own API key (Komga → Account settings → API keys). Komga only exposes progress to the user itself."}}
}

func (m *Module) TestAccount(ctx context.Context, acc library.Account) (string, error) {
	u, err := m.me(ctx, acc.Credentials["apiKey"])
	if err != nil {
		return "", err
	}
	return u.Email, nil
}

type pageOf[T any] struct {
	Content []T `json:"content"`
}

type komgaSeries struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type komgaBook struct {
	SeriesID     string `json:"seriesId"`
	URL          string `json:"url"`
	ReadProgress *struct {
		Page      int        `json:"page"`
		Completed bool       `json:"completed"`
		ReadDate  *time.Time `json:"readDate"`
	} `json:"readProgress"`
}

func is(field, value string) map[string]any {
	return map[string]any{field: map[string]any{"operator": "is", "value": value}}
}

func (m *Module) ReadProgress(ctx context.Context, acc library.Account, localRoots []string) ([]library.BookProgress, error) {
	libs, err := m.libraries(ctx)
	if err != nil {
		return nil, err
	}
	var libConds []any
	seriesDir := map[string]string{}
	for _, l := range libs {
		localRoot := m.pm.ToLocal(l.Root)
		relevant := false
		for _, r := range localRoots {
			if library.Under(localRoot, r) || library.Under(r, localRoot) {
				relevant = true
			}
		}
		if !relevant {
			continue
		}
		libConds = append(libConds, is("libraryId", l.ID))
		var series pageOf[komgaSeries]
		if err := m.do(ctx, m.s.APIKey, http.MethodPost, "/api/v1/series/list?unpaged=true",
			map[string]any{"condition": is("libraryId", l.ID)}, &series); err != nil {
			return nil, fmt.Errorf("list series: %w", err)
		}
		for _, s := range series.Content {
			seriesDir[s.ID] = m.pm.ToLocal(s.URL)
		}
	}
	if len(libConds) == 0 {
		return nil, nil
	}
	cond := map[string]any{"allOf": []any{
		map[string]any{"anyOf": libConds},
		map[string]any{"anyOf": []any{is("readStatus", "READ"), is("readStatus", "IN_PROGRESS")}},
	}}
	var books pageOf[komgaBook]
	if err := m.do(ctx, acc.Credentials["apiKey"], http.MethodPost, "/api/v1/books/list?unpaged=true", map[string]any{"condition": cond}, &books); err != nil {
		return nil, fmt.Errorf("list books: %w", err)
	}
	out := make([]library.BookProgress, 0, len(books.Content))
	for _, b := range books.Content {
		dir, ok := seriesDir[b.SeriesID]
		if !ok || b.ReadProgress == nil {
			continue
		}
		// non-admin users only see the file name; join with the series folder
		out = append(out, library.BookProgress{LocalPath: path.Join(dir, path.Base(strings.ReplaceAll(b.URL, "\\", "/"))),
			Completed: b.ReadProgress.Completed, Page: b.ReadProgress.Page, ReadAt: b.ReadProgress.ReadDate})
	}
	return out, nil
}

var _ library.ProgressReader = (*Module)(nil)
