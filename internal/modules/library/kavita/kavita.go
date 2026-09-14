// Package kavita implements the Kavita library module: per-folder scans and
// per-user read progress.
package kavita

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/httpx"
	"github.com/Asion001/mangarr/internal/modules/library"
)

type Settings struct {
	URL          string            `json:"url" label:"Kavita URL" type:"url" required:"true" placeholder:"http://kavita:5000" order:"1"`
	APIKey       string            `json:"apiKey" label:"Admin API key" secret:"true" required:"true" order:"2" help:"API key of a Kavita admin (User settings → 3rd party clients)."`
	PathMappings map[string]string `json:"pathMappings" label:"Path mappings" type:"keyvalue" order:"3" help:"mangarr path → Kavita path, when Kavita mounts the library at a different path."`
}

func init() {
	modules.Register(&modules.Implementation{
		Kind: modules.KindLibrary, Name: "kavita", DisplayName: "Kavita",
		Description: "Scans changed series folders in Kavita and reads per-user progress for read-based cleanup.",
		InfoURL:     "https://www.kavitareader.com",
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

type userDTO struct {
	Username string `json:"username"`
	Token    string `json:"token"`
}

func (m *Module) authenticate(ctx context.Context, apiKey string) (*userDTO, error) {
	var u userDTO
	q := "/api/Plugin/authenticate?apiKey=" + url.QueryEscape(apiKey) + "&pluginName=mangarr"
	if err := httpx.Do(ctx, m.http, http.MethodPost, httpx.Join(m.s.URL, q), nil, nil, &u); err != nil {
		return nil, fmt.Errorf("kavita authentication failed: %w", err)
	}
	return &u, nil
}

func (m *Module) Test(ctx context.Context) error {
	_, err := m.authenticate(ctx, m.s.APIKey)
	return err
}

func (m *Module) Rescan(ctx context.Context, localPaths []string) error {
	for _, lp := range localPaths {
		body := map[string]any{"apiKey": m.s.APIKey, "folderPath": m.pm.ToRemote(lp), "abortOnNoSeriesMatch": false}
		if err := httpx.Do(ctx, m.http, http.MethodPost, httpx.Join(m.s.URL, "/api/Library/scan-folder"), nil, body, nil); err != nil {
			return fmt.Errorf("scan %s: %w", lp, err)
		}
	}
	return nil
}

func (m *Module) AccountFields() []modules.Field {
	return []modules.Field{{Name: "apiKey", Label: "Kavita API key", Type: "password", Secret: true, Required: true,
		Help: "The reader's own API key (Kavita → User settings → 3rd party clients)."}}
}

func (m *Module) TestAccount(ctx context.Context, acc library.Account) (string, error) {
	u, err := m.authenticate(ctx, acc.Credentials["apiKey"])
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

type seriesDTO struct {
	ID         int    `json:"id"`
	PagesRead  int    `json:"pagesRead"`
	FolderPath string `json:"folderPath"`
}

type volumeDTO struct {
	Chapters []struct {
		Pages                  int    `json:"pages"`
		PagesRead              int    `json:"pagesRead"`
		LastReadingProgressUtc string `json:"lastReadingProgressUtc"`
		Files                  []struct {
			FilePath string `json:"filePath"`
		} `json:"files"`
	} `json:"chapters"`
}

func (m *Module) ReadProgress(ctx context.Context, acc library.Account, localRoots []string) ([]library.BookProgress, error) {
	u, err := m.authenticate(ctx, acc.Credentials["apiKey"])
	if err != nil {
		return nil, err
	}
	auth := map[string]string{"Authorization": "Bearer " + u.Token}
	var out []library.BookProgress
	for page := 1; page < 100; page++ {
		var series []seriesDTO
		q := fmt.Sprintf("/api/Series/all-v2?PageNumber=%d&PageSize=500", page)
		filter := map[string]any{"statements": []any{}, "combination": 1, "limitTo": 0}
		if err := httpx.Do(ctx, m.http, http.MethodPost, httpx.Join(m.s.URL, q), auth, filter, &series); err != nil {
			return nil, fmt.Errorf("list series: %w", err)
		}
		for _, s := range series {
			if s.PagesRead == 0 {
				continue
			}
			local := m.pm.ToLocal(s.FolderPath)
			inRoots := false
			for _, r := range localRoots {
				if library.Under(local, r) {
					inRoots = true
				}
			}
			if !inRoots {
				continue
			}
			var vols []volumeDTO
			if err := httpx.Do(ctx, m.http, http.MethodGet, httpx.Join(m.s.URL, fmt.Sprintf("/api/Series/volumes?seriesId=%d", s.ID)), auth, nil, &vols); err != nil {
				return nil, fmt.Errorf("series %d volumes: %w", s.ID, err)
			}
			for _, v := range vols {
				for _, c := range v.Chapters {
					if c.PagesRead == 0 {
						continue
					}
					readAt := parseDotNetTime(c.LastReadingProgressUtc)
					for _, f := range c.Files {
						out = append(out, library.BookProgress{LocalPath: m.pm.ToLocal(f.FilePath),
							Completed: c.Pages > 0 && c.PagesRead >= c.Pages, Page: c.PagesRead, ReadAt: readAt})
					}
				}
			}
		}
		if len(series) < 500 {
			break
		}
	}
	return out, nil
}

var _ library.ProgressReader = (*Module)(nil)

// parseDotNetTime parses .NET DateTime JSON, which may lack a time zone
// ("2026-01-02T03:04:05.1234567"); such values are UTC in Kavita's *Utc fields.
func parseDotNetTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.9999999", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			if t.Year() <= 1 {
				return nil
			}
			t = t.UTC()
			return &t
		}
	}
	return nil
}
