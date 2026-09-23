package sites

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// Scans.gg has a JSON API for everything; images come from its CDN. As in
// the extension, a manga's link is its bare numeric id and a chapter's is the
// API path that lists its pages, so everything later requests is in the link.
var sggID = sourcekit.KeiyoushiID("ScansGG", "en", 1)

const (
	sggSite = "https://scans.gg"
	sggAPI  = "https://api.scans.gg"
	sggCDN  = "https://cdn.scans.gg/uploads"

	sggPageSize    = 21
	sggLatestSize  = 14
	sggChapterSize = 100
)

func init() {
	sourcekit.Register(sggID, func(d sourcekit.Deps) sourcekit.Site {
		return &scansgg{c: d.Client, site: sggSite, api: sggAPI, cdn: sggCDN}
	})
}

type scansgg struct {
	c *sourcekit.Client
	// site, api and cdn are the addresses to talk to (tests point them at a
	// recorded copy).
	site, api, cdn string
}

func (s *scansgg) Info() sourcekit.Info {
	return sourcekit.Info{ID: sggID, Name: "Scans.gg", Lang: "en", BaseURL: s.site, SupportsBrowse: true,
		IconURL: s.site + "/favicon.ico"}
}

// Politeness: the extension sets no limit of its own, so stay gentle.
func (s *scansgg) Politeness() sourcekit.Politeness {
	return sourcekit.Politeness{RequestsPerMinute: 60, MaxConcurrent: 2}
}

func (s *scansgg) headers() map[string]string {
	return map[string]string{"Referer": s.site + "/", "Origin": s.site}
}

// ---- the API's shapes (only the fields we use) ------------------------------

type sggMeta struct {
	HasMore bool `json:"has_more"`
}

type sggSeries struct {
	ID      int      `json:"id"`
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Cover   string   `json:"cover"`
	Author  []string `json:"author"`
	Artist  []string `json:"artist"`
	Tags    []int    `json:"tags"`
	Status  *int     `json:"status"`
}

type sggChapter struct {
	ID        int     `json:"id"`
	Number    float64 `json:"number"`
	Title     string  `json:"title"`
	CreatedAt string  `json:"created_at"`
	GroupID   *int    `json:"group_id"`
	Group     *struct {
		Title string `json:"title"`
	} `json:"group"`
}

// sggTags are the site's genre ids, as the extension lists them.
var sggTags = map[int]string{
	49: "Regression", 48: "Male Protagonist", 47: "Survival", 46: "Avant Garde", 45: "Award Winning",
	44: "Lolicon", 43: "Mahou Shoujo", 42: "Doujinshi", 41: "Girls Love", 40: "Hentai", 39: "Mecha",
	38: "Shotacon", 37: "Ecchi", 36: "Music", 35: "Smut", 34: "Erotica", 33: "Adult", 32: "Gourmet",
	31: "Yuri", 30: "Shoujo Ai", 29: "Yaoi", 28: "Shounen Ai", 27: "Boys Love", 26: "Harem", 25: "Tragedy",
	24: "Gender Bender", 23: "Suspense", 22: "Psychological", 21: "Mature", 20: "Horror", 19: "Mystery",
	18: "Martial Arts", 17: "Sci-fi", 16: "Adventure", 15: "Supernatural", 14: "Sports", 13: "Shounen",
	12: "Historical", 11: "Seinen", 10: "Action", 9: "Josei", 8: "Thriller", 7: "School Life",
	6: "Slice Of Life", 5: "Drama", 4: "Comedy", 3: "Shoujo", 2: "Romance", 1: "Fantasy",
}

// ---- lists ------------------------------------------------------------------

func (s *scansgg) Search(ctx context.Context, query string, page int) (sourcekit.Results, error) {
	q := s.seriesQuery(page)
	if query = strings.TrimSpace(query); query != "" {
		q.Set("q", query)
	}
	// the extension always sends the (empty) filters
	q.Set("q_type", "[]")
	q.Set("q_status", "[]")
	q.Set("q_tags", "[]")
	return s.series(ctx, q)
}

func (s *scansgg) Popular(ctx context.Context, page int) (sourcekit.Results, error) {
	return s.series(ctx, s.seriesQuery(page))
}

func (s *scansgg) seriesQuery(page int) url.Values {
	if page < 1 {
		page = 1
	}
	return url.Values{"limit": {strconv.Itoa(sggPageSize)}, "offset": {strconv.Itoa((page - 1) * sggPageSize)}}
}

func (s *scansgg) series(ctx context.Context, q url.Values) (sourcekit.Results, error) {
	var out struct {
		Data []sggSeries `json:"data"`
	}
	if err := s.c.JSON(ctx, sourcekit.Request{URL: s.api + "/series", Query: q, Headers: s.headers()}, &out); err != nil {
		return sourcekit.Results{}, err
	}
	res := s.results(out.Data)
	// /series has no pagination meta: a full page means there may be more
	res.HasNext = len(out.Data) == sggPageSize
	return res, nil
}

// Latest lists recently updated series (the chapters endpoint, asked to
// answer with the series).
func (s *scansgg) Latest(ctx context.Context, page int) (sourcekit.Results, error) {
	if page < 1 {
		page = 1
	}
	q := url.Values{"page": {strconv.Itoa(page)}, "limit": {strconv.Itoa(sggLatestSize)}, "chapters": {"true"},
		"series_details": {"true"}, "group_details": {"true"}, "sort": {"date"}}
	var out struct {
		Data []sggSeries `json:"data"`
		Meta *sggMeta    `json:"meta"`
	}
	if err := s.c.JSON(ctx, sourcekit.Request{URL: s.api + "/chapters", Query: q, Headers: s.headers()}, &out); err != nil {
		return sourcekit.Results{}, err
	}
	res := s.results(out.Data)
	res.HasNext = out.Meta != nil && out.Meta.HasMore
	return res, nil
}

func (s *scansgg) results(list []sggSeries) sourcekit.Results {
	var res sourcekit.Results
	for _, x := range list {
		id := strconv.Itoa(x.ID)
		if x.ID == 0 || res.Has(id) {
			continue
		}
		res.Mangas = append(res.Mangas, sourcekit.Manga{URL: id, ID: id, Title: strings.TrimSpace(x.Title), CoverURL: s.cover(x.Cover)})
	}
	return res
}

func (s *scansgg) cover(file string) string {
	if file == "" {
		return ""
	}
	return s.cdn + "/covers/" + file
}

// ---- one manga --------------------------------------------------------------

func (s *scansgg) Details(ctx context.Context, ref sourcekit.Ref) (sourcekit.Details, error) {
	id := sggSeriesID(ref)
	if id == "" {
		return sourcekit.Details{}, fmt.Errorf("%q is not a series id", ref.URL)
	}
	q := url.Values{"id": {id}, "trackers": {"true"}, "sources": {"true"}}
	var out struct {
		Data *sggSeries `json:"data"`
	}
	if err := s.c.JSON(ctx, sourcekit.Request{URL: s.api + "/series", Query: q, Headers: s.headers()}, &out); err != nil {
		return sourcekit.Details{}, notFound(err)
	}
	x := out.Data
	if x == nil || x.ID == 0 {
		return sourcekit.Details{}, fmt.Errorf("%w: series %s", sourcekit.ErrNotFound, id)
	}
	d := sourcekit.Details{Manga: sourcekit.Manga{URL: id, ID: id, Title: strings.TrimSpace(x.Title), CoverURL: s.cover(x.Cover)},
		Author: strings.Join(x.Author, ", "), Artist: strings.Join(x.Artist, ", "),
		Description: strings.TrimSpace(x.Summary), Status: sourcekit.StatusUnknown, WebURL: s.site + "/series/" + id}
	for _, t := range x.Tags {
		if name, ok := sggTags[t]; ok {
			d.Genres = append(d.Genres, name)
		}
	}
	if x.Status != nil {
		switch *x.Status {
		case 1:
			d.Status = sourcekit.StatusOngoing
		case 2:
			d.Status = sourcekit.StatusCompleted
		case 3, 4, 5: // hiatus, cancelled and dropped: the extension calls them all cancelled
			d.Status = sourcekit.StatusCancelled
		}
	}
	return d, nil
}

func (s *scansgg) Chapters(ctx context.Context, ref sourcekit.Ref) ([]sourcekit.Chapter, error) {
	id := sggSeriesID(ref)
	if id == "" {
		return nil, fmt.Errorf("%q is not a series id", ref.URL)
	}
	var chapters []sourcekit.Chapter
	for page := 1; ; page++ {
		q := url.Values{"series_id": {id}, "limit": {strconv.Itoa(sggChapterSize)}, "page": {strconv.Itoa(page)},
			"group_details": {"true"}}
		var out struct {
			Data []sggChapter `json:"data"`
			Meta *sggMeta     `json:"meta"`
		}
		if err := s.c.JSON(ctx, sourcekit.Request{URL: s.api + "/chapters", Query: q, Headers: s.headers()}, &out); err != nil {
			return nil, notFound(err)
		}
		for _, c := range out.Data {
			chapters = append(chapters, s.chapter(id, c))
		}
		if out.Meta == nil || !out.Meta.HasMore || len(out.Data) == 0 {
			break
		}
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("%w: no chapters listed", sourcekit.ErrNotFound)
	}
	return chapters, nil
}

func (s *scansgg) chapter(seriesID string, c sggChapter) sourcekit.Chapter {
	group := 0
	if c.GroupID != nil {
		group = *c.GroupID
	}
	path := fmt.Sprintf("/chapter-navigation?series_id=%s&chapter_id=%d&group_id=%d", seriesID, c.ID, group)
	name := "Chapter " + strconv.FormatFloat(c.Number, 'f', -1, 32)
	if c.Title != "" {
		name += " - " + c.Title
	}
	ch := sourcekit.Chapter{URL: path, ID: strconv.Itoa(c.ID), Name: name, Number: c.Number,
		WebURL: s.site + "/series/" + seriesID}
	if c.Group != nil {
		ch.Scanlator = c.Group.Title
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", c.CreatedAt, time.UTC); err == nil {
		ch.UploadedAt = &t
	}
	return ch
}

func (s *scansgg) Pages(ctx context.Context, ch sourcekit.PageRef) ([]sourcekit.PageImage, error) {
	path := sourcekit.Path(ch.URL)
	if !strings.HasPrefix(path, "/chapter-navigation?") {
		return nil, fmt.Errorf("%q is not a chapter url", ch.URL)
	}
	var out struct {
		Data struct {
			Chapter *struct {
				ID    int `json:"id"`
				Pages []struct {
					Position int    `json:"position"`
					Path     string `json:"path"`
				} `json:"pages"`
			} `json:"chapter"`
		} `json:"data"`
	}
	if err := s.c.JSON(ctx, sourcekit.Request{URL: s.api + path, Headers: s.headers()}, &out); err != nil {
		return nil, notFound(err)
	}
	c := out.Data.Chapter
	if c == nil || c.ID == 0 || len(c.Pages) == 0 {
		return nil, fmt.Errorf("%w: the chapter has no pages", sourcekit.ErrNotFound)
	}
	list := c.Pages
	sort.SliceStable(list, func(i, j int) bool { return list[i].Position < list[j].Position })
	pages := make([]sourcekit.PageImage, 0, len(list))
	for _, p := range list {
		pages = append(pages, sourcekit.PageImage{Index: len(pages), URL: fmt.Sprintf("%s/pages/%d/%s", s.cdn, c.ID, p.Path),
			Headers: map[string]string{"Referer": s.site + "/"}})
	}
	return pages, nil
}

// sggSeriesID is the series' numeric id: the link itself, or the last part of
// a site address ("/series/123").
func sggSeriesID(ref sourcekit.Ref) string {
	p := strings.Trim(sourcekit.Path(ref.URL), "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	if _, err := strconv.Atoi(p); err == nil {
		return p
	}
	return ref.ID
}
