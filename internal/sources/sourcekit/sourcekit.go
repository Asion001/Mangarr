// Package sourcekit is the toolkit for mangarr's own source sites: the Site
// interface a site implements, the types it speaks in, and the HTTP helpers
// it uses. It deliberately depends on nothing else in mangarr, so the sites
// (and this kit) can move to a repository of their own.
package sourcekit

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrUnsupported means a site can't do this (browse, preferences, ...).
var ErrUnsupported = errors.New("not supported by this site")

// ErrNotFound means the manga or chapter is gone from the site.
var ErrNotFound = errors.New("not found at the site")

// Info describes a site. ID must stay stable forever: it identifies the
// catalog in every library, and matching the id the Mihon/Tachiyomi
// extension for the same site uses keeps backup imports working.
type Info struct {
	ID      string
	Name    string
	Lang    string
	BaseURL string
	NSFW    bool
	// SupportsBrowse: the site implements Browser (popular and latest).
	SupportsBrowse bool
	IconURL        string
}

// Ref identifies a manga at a site by its path or URL ("/manga/<id>").
type Ref struct {
	URL string
	// Title helps a site re-find a manga whose URL changed.
	Title string
}

// Manga is one search or browse result.
type Manga struct {
	URL      string
	Title    string
	CoverURL string
	// Chapters is the chapter count when the site already knows it.
	Chapters *int
}

// Status values a site may report.
const (
	StatusUnknown   = "unknown"
	StatusOngoing   = "ongoing"
	StatusCompleted = "completed"
	StatusHiatus    = "hiatus"
	StatusCancelled = "cancelled"
)

// Details is everything a site knows about one manga.
type Details struct {
	Manga
	Author      string
	Artist      string
	Description string
	Genres      []string
	Status      string
	WebURL      string
}

// Chapter is one chapter of a manga.
type Chapter struct {
	URL       string
	Name      string
	Scanlator string
	// Number as the site reports it (-1 when it doesn't).
	Number     float64
	UploadedAt *time.Time
	WebURL     string
}

// PageImage is one page: where to get it, and what a request for it needs.
// Headers matter when the site checks the Referer, and they let another
// machine (a worker) fetch the page itself.
type PageImage struct {
	Index   int
	URL     string
	Headers map[string]string
}

// Results is one page of results.
type Results struct {
	Mangas  []Manga
	HasNext bool
}

// Has reports whether a result for this manga is already in the list, so a
// site scraping a page with repeated links doesn't return it twice.
func (r *Results) Has(url string) bool {
	for _, m := range r.Mangas {
		if m.URL == url {
			return true
		}
	}
	return false
}

// Site is what a site implements. Everything optional is a separate
// interface, so a small site stays small.
type Site interface {
	Info() Info
	Search(ctx context.Context, query string, page int) (Results, error)
	Details(ctx context.Context, ref Ref) (Details, error)
	Chapters(ctx context.Context, ref Ref) ([]Chapter, error)
	Pages(ctx context.Context, chapterURL string) ([]PageImage, error)
}

// Browser is implemented by sites with popular and latest listings.
type Browser interface {
	Popular(ctx context.Context, page int) (Results, error)
	Latest(ctx context.Context, page int) (Results, error)
}

// Configurable is implemented by sites with their own options (a language, a
// content rating). Values are strings, bools or numbers.
type Configurable interface {
	Options() []Option
	SetOption(key string, value any) error
}

// Option is one of a site's settings.
type Option struct {
	Key     string
	Title   string
	Help    string
	Type    string // switch, text, select, multiselect
	Value   any
	Choices []Choice
}

// Choice is one value of a select option.
type Choice struct {
	Value string
	Label string
}

// Politeness is how hard a site may be hit; the core turns it into limits.
type Politeness struct {
	RequestsPerMinute int
	MaxConcurrent     int
}

// Polite is implemented by sites that know their own limits.
type Polite interface {
	Politeness() Politeness
}

// Builder makes a site. Sites register one in init().
type Builder func(deps Deps) Site

// Deps are what the core gives a site: an HTTP client that already carries a
// cookie jar, a browser user agent and the challenge solver.
type Deps struct {
	Client *Client
}

var (
	mu       sync.Mutex
	builders = map[string]Builder{}
)

// Register adds a site to the registry (called from a site's init).
// Registering the same id twice panics: ids must be unique and stable.
func Register(id string, b Builder) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := builders[id]; dup {
		panic("sourcekit: site " + id + " registered twice")
	}
	builders[id] = b
}

// Build makes every registered site.
func Build(d Deps) []Site {
	mu.Lock()
	ids := make([]string, 0, len(builders))
	for id := range builders {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Site, 0, len(ids))
	for _, id := range ids {
		out = append(out, builders[id](d))
	}
	mu.Unlock()
	return out
}

// Registered lists the ids of every known site.
func Registered() []string {
	mu.Lock()
	defer mu.Unlock()
	ids := make([]string, 0, len(builders))
	for id := range builders {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
