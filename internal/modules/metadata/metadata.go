// Package metadata defines metadata provider modules (AniList, MangaUpdates,
// MangaDex, ComicVine, ...). The core queries all enabled providers by
// priority and merges their results (see internal/metadataagg).
package metadata

import (
	"context"

	"github.com/Asion001/mangarr/internal/modules"
)

// SeriesMetadata is what a provider knows about a series.
type SeriesMetadata struct {
	Provider      string            `json:"provider"` // implementation name, e.g. "anilist"
	ID            string            `json:"id"`
	Title         string            `json:"title"`
	AltTitles     []string          `json:"altTitles,omitempty"`
	Description   string            `json:"description,omitempty"`
	Status        string            `json:"status,omitempty"` // ongoing/completed/hiatus/cancelled/unknown
	Year          int               `json:"year,omitempty"`
	Authors       []string          `json:"authors,omitempty"`
	Artists       []string          `json:"artists,omitempty"`
	Genres        []string          `json:"genres,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	Publisher     string            `json:"publisher,omitempty"`
	CoverURL      string            `json:"coverUrl,omitempty"`
	Links         map[string]string `json:"links,omitempty"`
	ExternalIDs   map[string]string `json:"externalIds,omitempty"` // cross ids: "mal" -> "13", "mangaupdates" -> "..."
	Adult         bool              `json:"adult,omitempty"`
	TotalChapters int               `json:"totalChapters,omitempty"`
	Format        string            `json:"format,omitempty"`  // manga, manhwa, manhua, comic, oneshot, novel
	Country       string            `json:"country,omitempty"` // ISO country of origin
	URL           string            `json:"url,omitempty"`
}

type Module interface {
	modules.Instance
	Search(ctx context.Context, query string, limit int) ([]SeriesMetadata, error)
	Get(ctx context.Context, id string) (*SeriesMetadata, error)
}

// ExternalLookup is implemented by providers that can resolve a series from
// another provider's id (e.g. AniList by MAL id). Used to join providers.
type ExternalLookup interface {
	LookupExternal(ctx context.Context, provider, id string) (*SeriesMetadata, error)
}
