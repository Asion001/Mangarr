// Package anilist implements a metadata module backed by the public AniList
// GraphQL API (no API key required).
package anilist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/metadata"
)

const defaultEndpoint = "https://graphql.anilist.co"

type Settings struct {
	TitleLanguage  string `json:"titleLanguage" label:"Title language" options:"english:English (fallback romaji),romaji:Romaji,native:Native,userPreferred:AniList default" order:"1"`
	MinTagRank     int    `json:"minTagRank" label:"Minimum tag rank" order:"2" help:"Only AniList tags with at least this relevance (0-100) are imported." advanced:"true"`
	IncludeSpoiler bool   `json:"includeSpoilerTags" label:"Include spoiler tags" order:"3" advanced:"true"`
	Endpoint       string `json:"endpoint" label:"API endpoint" type:"url" order:"4" advanced:"true"`
}

func init() {
	modules.Register(&modules.Implementation{
		Kind:        modules.KindMetadata,
		Name:        "anilist",
		DisplayName: "AniList",
		Description: "Series metadata from AniList: titles, synonyms, synopsis, staff, genres, tags, cover and status.",
		InfoURL:     "https://anilist.co",
		Settings: func() any {
			return &Settings{TitleLanguage: "english", MinTagRank: 60, Endpoint: defaultEndpoint}
		},
		New: func(deps modules.Deps, s any) (modules.Instance, error) {
			hc := deps.HTTP
			if hc == nil {
				hc = &http.Client{Timeout: 30 * time.Second}
			}
			st := s.(*Settings)
			if st.Endpoint == "" {
				st.Endpoint = defaultEndpoint
			}
			return &Module{s: st, http: hc}, nil
		},
	})
}

type Module struct {
	s    *Settings
	http *http.Client

	mu      sync.Mutex
	lastReq time.Time
}

const mediaFields = `id idMal title { romaji english native userPreferred } synonyms status format countryOfOrigin isAdult
chapters startDate { year } genres tags { name rank isMediaSpoiler } coverImage { extraLarge large }
siteUrl description(asHtml: false) staff(perPage: 8) { edges { role node { name { full } } } }
externalLinks { site url type }`

type media struct {
	ID    int `json:"id"`
	IDMal int `json:"idMal"`
	Title struct {
		Romaji        string `json:"romaji"`
		English       string `json:"english"`
		Native        string `json:"native"`
		UserPreferred string `json:"userPreferred"`
	} `json:"title"`
	Synonyms        []string `json:"synonyms"`
	Status          string   `json:"status"`
	Format          string   `json:"format"`
	CountryOfOrigin string   `json:"countryOfOrigin"`
	IsAdult         bool     `json:"isAdult"`
	Chapters        int      `json:"chapters"`
	StartDate       struct {
		Year int `json:"year"`
	} `json:"startDate"`
	Genres []string `json:"genres"`
	Tags   []struct {
		Name      string `json:"name"`
		Rank      int    `json:"rank"`
		IsSpoiler bool   `json:"isMediaSpoiler"`
	} `json:"tags"`
	CoverImage struct {
		ExtraLarge string `json:"extraLarge"`
		Large      string `json:"large"`
	} `json:"coverImage"`
	SiteURL     string `json:"siteUrl"`
	Description string `json:"description"`
	Staff       struct {
		Edges []struct {
			Role string `json:"role"`
			Node struct {
				Name struct {
					Full string `json:"full"`
				} `json:"name"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"staff"`
	ExternalLinks []struct {
		Site string `json:"site"`
		URL  string `json:"url"`
		Type string `json:"type"`
	} `json:"externalLinks"`
}

func (m *Module) query(ctx context.Context, q string, vars map[string]any, out any) error {
	// AniList allows ~30-90 requests/minute; keep a small gap between calls.
	m.mu.Lock()
	if wait := 700*time.Millisecond - time.Since(m.lastReq); wait > 0 {
		time.Sleep(wait)
	}
	m.lastReq = time.Now()
	m.mu.Unlock()

	body, _ := json.Marshal(map[string]any{"query": q, "variables": vars})
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.s.Endpoint, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		resp, err := m.http.Do(req)
		if err != nil {
			return fmt.Errorf("anilist: %w", err)
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests && attempt < 2 {
			retry, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			if retry <= 0 || retry > 60 {
				retry = 10
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(retry) * time.Second):
			}
			continue
		}
		var env struct {
			Data   json.RawMessage `json:"data"`
			Errors []struct {
				Message string `json:"message"`
				Status  int    `json:"status"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("anilist: HTTP %d", resp.StatusCode)
		}
		if len(env.Errors) > 0 {
			if env.Errors[0].Status == http.StatusNotFound {
				return metadata.ErrNotFound
			}
			return fmt.Errorf("anilist: %s", env.Errors[0].Message)
		}
		return json.Unmarshal(env.Data, out)
	}
}

func (m *Module) Test(ctx context.Context) error {
	_, err := m.Search(ctx, "One Piece", 1)
	return err
}

func (m *Module) Search(ctx context.Context, q string, limit int) ([]metadata.SeriesMetadata, error) {
	if limit <= 0 || limit > 25 {
		limit = 10
	}
	var out struct {
		Page struct {
			Media []media `json:"media"`
		} `json:"Page"`
	}
	err := m.query(ctx, `query ($s: String, $n: Int) { Page(perPage: $n) { media(search: $s, type: MANGA, sort: SEARCH_MATCH) { `+mediaFields+` } } }`,
		map[string]any{"s": q, "n": limit}, &out)
	if err != nil {
		return nil, err
	}
	res := make([]metadata.SeriesMetadata, 0, len(out.Page.Media))
	for _, md := range out.Page.Media {
		res = append(res, m.convert(md))
	}
	return res, nil
}

func (m *Module) Get(ctx context.Context, id string) (*metadata.SeriesMetadata, error) {
	n, err := strconv.Atoi(id)
	if err != nil {
		return nil, fmt.Errorf("invalid AniList id %q", id)
	}
	var out struct {
		Media media `json:"Media"`
	}
	if err := m.query(ctx, `query ($id: Int) { Media(id: $id, type: MANGA) { `+mediaFields+` } }`, map[string]any{"id": n}, &out); err != nil {
		return nil, err
	}
	md := m.convert(out.Media)
	return &md, nil
}

func (m *Module) LookupExternal(ctx context.Context, provider, id string) (*metadata.SeriesMetadata, error) {
	if provider == "anilist" {
		return m.Get(ctx, id)
	}
	if provider != "mal" {
		return nil, metadata.ErrNotFound
	}
	n, err := strconv.Atoi(id)
	if err != nil {
		return nil, metadata.ErrNotFound
	}
	var out struct {
		Media media `json:"Media"`
	}
	if err := m.query(ctx, `query ($id: Int) { Media(idMal: $id, type: MANGA) { `+mediaFields+` } }`, map[string]any{"id": n}, &out); err != nil {
		return nil, err
	}
	md := m.convert(out.Media)
	return &md, nil
}

var (
	htmlTags   = regexp.MustCompile(`(?i)<br\s*/?>`)
	anyTag     = regexp.MustCompile(`<[^>]+>`)
	blankLines = regexp.MustCompile(`\n{3,}`)
)

func cleanDescription(s string) string {
	s = htmlTags.ReplaceAllString(s, "\n")
	s = anyTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.TrimSpace(blankLines.ReplaceAllString(s, "\n\n"))
}

func (m *Module) convert(md media) metadata.SeriesMetadata {
	title := md.Title.UserPreferred
	switch m.s.TitleLanguage {
	case "english":
		title = firstNonEmpty(md.Title.English, md.Title.Romaji, md.Title.UserPreferred)
	case "romaji":
		title = firstNonEmpty(md.Title.Romaji, md.Title.UserPreferred)
	case "native":
		title = firstNonEmpty(md.Title.Native, md.Title.Romaji)
	}
	alt := []string{}
	for _, t := range append([]string{md.Title.English, md.Title.Romaji, md.Title.Native}, md.Synonyms...) {
		if t != "" && t != title {
			alt = append(alt, t)
		}
	}
	out := metadata.SeriesMetadata{
		Provider: "anilist", ID: strconv.Itoa(md.ID), Title: title, AltTitles: dedupe(alt),
		Description: cleanDescription(md.Description), Year: md.StartDate.Year, Genres: md.Genres,
		CoverURL: firstNonEmpty(md.CoverImage.ExtraLarge, md.CoverImage.Large), URL: md.SiteURL,
		Adult: md.IsAdult, TotalChapters: md.Chapters, Country: md.CountryOfOrigin,
		Links:       map[string]string{"AniList": md.SiteURL},
		ExternalIDs: map[string]string{"anilist": strconv.Itoa(md.ID)},
	}
	if md.IDMal > 0 {
		out.ExternalIDs["mal"] = strconv.Itoa(md.IDMal)
		out.Links["MyAnimeList"] = "https://myanimelist.net/manga/" + strconv.Itoa(md.IDMal)
	}
	for _, l := range md.ExternalLinks {
		if l.Type == "INFO" || l.Type == "STREAMING" {
			out.Links[l.Site] = l.URL
		}
	}
	switch md.Status {
	case "RELEASING", "NOT_YET_RELEASED":
		out.Status = "ongoing"
	case "FINISHED":
		out.Status = "completed"
	case "HIATUS":
		out.Status = "hiatus"
	case "CANCELLED":
		out.Status = "cancelled"
	default:
		out.Status = "unknown"
	}
	switch {
	case md.Format == "ONE_SHOT":
		out.Format = "oneshot"
	case md.Format == "NOVEL":
		out.Format = "novel"
	case md.CountryOfOrigin == "KR":
		out.Format = "manhwa"
	case md.CountryOfOrigin == "CN" || md.CountryOfOrigin == "TW":
		out.Format = "manhua"
	default:
		out.Format = "manga"
	}
	for _, t := range md.Tags {
		if t.Rank >= m.s.MinTagRank && (m.s.IncludeSpoiler || !t.IsSpoiler) {
			out.Tags = append(out.Tags, t.Name)
		}
	}
	for _, e := range md.Staff.Edges {
		role := strings.ToLower(e.Role)
		name := e.Node.Name.Full
		if strings.Contains(role, "story") || strings.Contains(role, "original creator") {
			out.Authors = append(out.Authors, name)
		}
		if strings.Contains(role, "art") {
			out.Artists = append(out.Artists, name)
		}
	}
	out.Authors, out.Artists = dedupe(out.Authors), dedupe(out.Artists)
	return out
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

func dedupe(xs []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, x := range xs {
		k := strings.ToLower(strings.TrimSpace(x))
		if k != "" && !seen[k] {
			seen[k] = true
			out = append(out, x)
		}
	}
	return out
}

var _ metadata.ExternalLookup = (*Module)(nil)
