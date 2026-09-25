// Package silo matches adaptations using Silo's native catalog API.
package silo

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/httpx"
	"github.com/Asion001/mangarr/internal/modules/mediaserver"
)

func init() {
	modules.Register(&modules.Implementation{
		Kind: modules.KindMediaServer, Name: "silo", DisplayName: "Silo",
		Description: "Links anime adaptations to your Silo catalog (title and year matching).",
		Settings:    func() any { return &mediaserver.Settings{} },
		New: func(deps modules.Deps, s any) (modules.Instance, error) {
			return &Module{s: *s.(*mediaserver.Settings), http: mediaserver.Client(deps.HTTP)}, nil
		},
	})
}

type Module struct {
	s       mediaserver.Settings
	http    *http.Client
	matches mediaserver.Cache[string]
}

type catalogPage struct {
	Items *[]struct {
		ID    string `json:"content_id"`
		Title string `json:"title"`
		Type  string `json:"type"`
		Year  int    `json:"year"`
	} `json:"items"`
	HasMore  bool   `json:"has_more"`
	Snapshot string `json:"snapshot"`
}

func (m *Module) catalog(ctx context.Context, q url.Values) (catalogPage, error) {
	var page catalogPage
	err := mediaserver.Get(ctx, m.http, httpx.Join(m.s.URL, "api/v1/catalog")+"?"+q.Encode(),
		map[string]string{"Authorization": "Bearer " + m.s.APIKey}, &page)
	if err == nil && page.Items == nil {
		err = errors.New("invalid Silo catalog response")
	}
	return page, err
}

func (m *Module) Test(ctx context.Context) error {
	_, err := m.catalog(ctx, url.Values{"source": {"query"}, "type": {"movie"}, "limit": {"1"}})
	return err
}

func (m *Module) Find(ctx context.Context, a model.Adaptation) (string, error) {
	return m.matches.Get(ctx, mediaserver.AdaptationKey(a), func(ctx context.Context) (string, error) {
		// Silo's native catalog currently exposes no AniList or MAL provider IDs.
		if a.Year <= 0 || mediaserver.NormalizeTitle(a.Title) == "" {
			return "", nil
		}
		kinds := []string{"movie", "series"}
		switch a.Format {
		case "movie":
			kinds = []string{"movie"}
		case "tv", "tv_short":
			kinds = []string{"series"}
		case "ova", "ona", "special":
		default:
			return "", nil
		}
		var items []mediaserver.Item
		for _, kind := range kinds {
			q := url.Values{"source": {"query"}, "type": {kind}, "q": {a.Title}, "limit": {"100"},
				"year_min": {strconv.Itoa(a.Year)}, "year_max": {strconv.Itoa(a.Year)}}
			for offset := 0; ; {
				if offset >= 10000 {
					return "", errors.New("Silo search exceeds 10000 items")
				}
				q.Set("offset", strconv.Itoa(offset))
				page, err := m.catalog(ctx, q)
				if err != nil {
					return "", err
				}
				for _, item := range *page.Items {
					items = append(items, mediaserver.Item{ID: item.ID, Title: item.Title, Year: item.Year, Type: item.Type,
						URL: httpx.Join(m.s.URL, "item/"+url.PathEscape(item.ID))})
				}
				if !page.HasMore {
					break
				}
				if len(*page.Items) == 0 {
					return "", errors.New("incomplete Silo catalog")
				}
				offset += len(*page.Items)
				if page.Snapshot != "" {
					q.Set("snapshot", page.Snapshot)
				}
			}
		}
		return mediaserver.Match(a, items), nil
	})
}
