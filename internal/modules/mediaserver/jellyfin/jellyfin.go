// Package jellyfin matches adaptations against Jellyfin's library.
package jellyfin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/httpx"
	"github.com/Asion001/mangarr/internal/modules/mediaserver"
)

func init() {
	modules.Register(&modules.Implementation{
		Kind: modules.KindMediaServer, Name: "jellyfin", DisplayName: "Jellyfin",
		Description: "Links anime adaptations to your Jellyfin library.",
		Settings:    func() any { return &mediaserver.Settings{} },
		New: func(deps modules.Deps, s any) (modules.Instance, error) {
			return &Module{s: *s.(*mediaserver.Settings), http: mediaserver.Client(deps.HTTP)}, nil
		},
	})
}

type Module struct {
	s       mediaserver.Settings
	http    *http.Client
	catalog mediaserver.Cache[*snapshot]
}

func (m *Module) get(ctx context.Context, path string, out any) error {
	return mediaserver.Get(ctx, m.http, httpx.Join(m.s.URL, path), map[string]string{"X-Emby-Token": m.s.APIKey}, out)
}

func (m *Module) Test(ctx context.Context) error {
	// This endpoint is authenticated, unlike System/Info/Public.
	var info struct {
		ID string `json:"Id"`
	}
	if err := m.get(ctx, "System/Info", &info); err != nil {
		return err
	}
	if info.ID == "" {
		return errors.New("invalid Jellyfin system information")
	}
	return nil
}

type snapshot struct {
	items   []mediaserver.Item
	matches mediaserver.Cache[string]
}

func (m *Module) Find(ctx context.Context, a model.Adaptation) (string, error) {
	catalog, err := m.catalog.Get(ctx, "catalog", func(ctx context.Context) (*snapshot, error) {
		items, err := m.load(ctx)
		if err != nil {
			return nil, err
		}
		return &snapshot{items: items}, nil
	})
	if err != nil {
		return "", err
	}
	return catalog.matches.Get(ctx, mediaserver.AdaptationKey(a), func(context.Context) (string, error) {
		return mediaserver.Match(a, catalog.items), nil
	})
}

// A library snapshot finds provider-ID matches even when the local title is
// entirely different. Jellyfin's Items endpoint has no generic provider-ID filter.
func (m *Module) load(ctx context.Context) ([]mediaserver.Item, error) {
	var items []mediaserver.Item
	for offset := 0; offset < 50000; {
		q := url.Values{"Recursive": {"true"}, "IncludeItemTypes": {"Series,Movie"},
			"Fields": {"ProviderIds,OriginalTitle"}, "ExcludeLocationTypes": {"Virtual"},
			"IsPlaceHolder": {"false"}, "EnableImages": {"false"}, "EnableUserData": {"false"},
			"SortBy": {"SortName"}, "SortOrder": {"Ascending"}, "Limit": {"500"}, "StartIndex": {strconv.Itoa(offset)}}
		var page struct {
			Items *[]struct {
				ID             string            `json:"Id"`
				Name           string            `json:"Name"`
				OriginalTitle  string            `json:"OriginalTitle"`
				Type           string            `json:"Type"`
				ProductionYear int               `json:"ProductionYear"`
				ProviderIDs    map[string]string `json:"ProviderIds"`
				LocationType   string            `json:"LocationType"`
				IsPlaceHolder  bool              `json:"IsPlaceHolder"`
			} `json:"Items"`
			TotalRecordCount int `json:"TotalRecordCount"`
		}
		if err := m.get(ctx, "Items?"+q.Encode(), &page); err != nil {
			return nil, err
		}
		if page.Items == nil {
			return nil, errors.New("invalid Jellyfin items response")
		}
		for _, item := range *page.Items {
			if item.LocationType == "Virtual" || item.IsPlaceHolder {
				continue
			}
			link := httpx.Join(m.s.URL, "web/index.html") + "#!/details?id=" + url.QueryEscape(item.ID)
			items = append(items, mediaserver.Item{ID: item.ID, Title: item.Name, OriginalTitle: item.OriginalTitle,
				Year: item.ProductionYear, Type: strings.ToLower(item.Type), ProviderIDs: item.ProviderIDs, URL: link})
		}
		offset += len(*page.Items)
		if offset >= page.TotalRecordCount {
			return items, nil
		}
		if len(*page.Items) == 0 {
			return nil, errors.New("incomplete Jellyfin catalog")
		}
	}
	return nil, errors.New("Jellyfin catalog exceeds 50000 items")
}
