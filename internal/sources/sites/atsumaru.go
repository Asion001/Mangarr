package sites

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// Atsumaru has a JSON API for everything the site itself reads, and a search
// index (Typesense) answering the same queries its search box does. Pages are
// served from its CDN, so a download never goes through the site.
var atsumaruID = sourcekit.KeiyoushiID("Atsumaru", "en", 2)

const (
	atsumaruSite = "https://atsu.moe"
	atsumaruCDN  = "https://cdn.atsu.moe"
	// atsumaruPageSize is what its own search asks for.
	atsumaruPageSize   = 40
	atsumaruBrowseSize = 40
)

func init() {
	sourcekit.Register(atsumaruID, func(d sourcekit.Deps) sourcekit.Site {
		return &atsumaru{c: d.Client, site: atsumaruSite, cdn: atsumaruCDN}
	})
}

type atsumaru struct {
	c *sourcekit.Client
	// site and cdn are the addresses to talk to (tests point them at a
	// recorded copy).
	site, cdn string
	// adult includes titles the site marks 18+.
	adult bool
}

func (a *atsumaru) Info() sourcekit.Info {
	return sourcekit.Info{ID: atsumaruID, Name: "Atsumaru", Lang: "en", BaseURL: a.site, SupportsBrowse: true,
		IconURL: a.site + "/favicon.ico"}
}

// Politeness: the site's own extension holds itself to two requests a second.
func (a *atsumaru) Politeness() sourcekit.Politeness {
	return sourcekit.Politeness{RequestsPerMinute: 120, MaxConcurrent: 2}
}

func (a *atsumaru) Options() []sourcekit.Option {
	return []sourcekit.Option{{Key: "adult", Title: "Include adult titles", Type: "switch", Value: a.adult,
		Help: "Off by default, as on the site itself."}}
}

func (a *atsumaru) SetOption(key string, value any) error {
	if key != "adult" {
		return fmt.Errorf("%w: no option %q", sourcekit.ErrUnsupported, key)
	}
	switch v := value.(type) {
	case bool:
		a.adult = v
	case string:
		a.adult = v == "true" || v == "1"
	default:
		return fmt.Errorf("%v is not a switch", value)
	}
	return nil
}

// ---- search -----------------------------------------------------------------

// atsumaruDoc is one manga in the search index; the site's browse endpoints
// answer with a smaller shape of their own.
type atsumaruDoc struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	EnglishTitle string `json:"englishTitle"`
	Poster       string `json:"poster"`
	ChapterCount int    `json:"chapterCount"`
}

func (a *atsumaru) Search(ctx context.Context, query string, page int) (sourcekit.Results, error) {
	if page < 1 {
		page = 1
	}
	q := strings.TrimSpace(query)
	if q == "" {
		q = "*"
	}
	filters := []string{"hidden:!=true", "medium:!=[`Novel`]", "views:>0",
		"(mbContentRating:=[`Safe`,`Suggestive`,`Erotica`] || mbContentRating:!=*)"}
	if !a.adult {
		filters = append(filters, "isAdult:=false")
	}
	values := url.Values{}
	values.Set("q", q)
	values.Set("filter_by", strings.Join(filters, " && "))
	values.Set("page", strconv.Itoa(page))
	values.Set("per_page", strconv.Itoa(atsumaruPageSize))
	if q != "*" {
		values.Set("query_by", "title,englishTitle,otherNames,authors")
		values.Set("query_by_weights", "4,3,2,1")
		values.Set("num_typos", "4,3,2,1")
	}
	var out struct {
		Found int `json:"found"`
		Hits  []struct {
			Document atsumaruDoc `json:"document"`
		} `json:"hits"`
	}
	req := sourcekit.Request{URL: a.site + "/collections/manga/documents/search", Query: values, Headers: a.headers("")}
	if err := a.c.JSON(ctx, req, &out); err != nil {
		return sourcekit.Results{}, err
	}
	var res sourcekit.Results
	for _, h := range out.Hits {
		d := h.Document
		if d.ID == "" {
			continue
		}
		m := sourcekit.Manga{URL: "/manga/" + d.ID, ID: d.ID, Title: atsumaruTitle(d.Title, d.EnglishTitle),
			CoverURL: a.image(d.Poster)}
		if d.ChapterCount > 0 {
			n := d.ChapterCount
			m.Chapters = &n
		}
		res.Mangas = append(res.Mangas, m)
	}
	res.HasNext = out.Found > page*atsumaruPageSize
	return res, nil
}

func (a *atsumaru) Popular(ctx context.Context, page int) (sourcekit.Results, error) {
	return a.browse(ctx, "/api/home2/popular", page, url.Values{"timeframe": {"daily"}})
}

func (a *atsumaru) Latest(ctx context.Context, page int) (sourcekit.Results, error) {
	return a.browse(ctx, "/api/home2/recentlyUpdated", page, nil)
}

func (a *atsumaru) browse(ctx context.Context, path string, page int, extra url.Values) (sourcekit.Results, error) {
	if page < 1 {
		page = 1
	}
	values := url.Values{}
	for k, v := range extra {
		values[k] = v
	}
	values.Set("offset", strconv.Itoa((page-1)*atsumaruBrowseSize))
	values.Set("limit", strconv.Itoa(atsumaruBrowseSize))
	values.Set("types", "Manga,Manwha,Manhua,OEL")
	values.Set("mediums", "Comic")
	if a.adult {
		values.Set("adult", "1")
	}
	var out struct {
		Items []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Image string `json:"image"`
		} `json:"items"`
	}
	if err := a.c.JSON(ctx, sourcekit.Request{URL: a.site + path, Query: values, Headers: a.headers("")}, &out); err != nil {
		return sourcekit.Results{}, err
	}
	var res sourcekit.Results
	for _, it := range out.Items {
		if it.ID == "" {
			continue
		}
		res.Mangas = append(res.Mangas, sourcekit.Manga{URL: "/manga/" + it.ID, ID: it.ID, Title: it.Title,
			CoverURL: a.image(it.Image)})
	}
	res.HasNext = len(out.Items) >= atsumaruBrowseSize
	return res, nil
}

// ---- one manga --------------------------------------------------------------

// atsumaruPage is the site's manga page, trimmed to what a library needs.
type atsumaruPage struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	EnglishTitle string `json:"englishTitle"`
	Synopsis     string `json:"synopsis"`
	Status       string `json:"status"`
	Poster       struct {
		Image string `json:"image"`
	} `json:"poster"`
	Authors []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"authors"`
	Genres []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Scanlators []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"scanlators"`
	// Atsumaru occasionally reports the latest fractional chapter number here
	// (for example 37.5), despite naming the field like an integer count.
	TotalChapterCount float64 `json:"totalChapterCount"`
}

func (a *atsumaru) page(ctx context.Context, id string) (*atsumaruPage, error) {
	var out struct {
		MangaPage *atsumaruPage `json:"mangaPage"`
	}
	req := sourcekit.Request{URL: a.site + "/api/manga/page", Query: url.Values{"id": {id}}, Headers: a.headers("")}
	if err := a.c.JSON(ctx, req, &out); err != nil {
		return nil, err
	}
	if out.MangaPage == nil || out.MangaPage.ID == "" {
		return nil, fmt.Errorf("%w: manga %s", sourcekit.ErrNotFound, id)
	}
	return out.MangaPage, nil
}

func (a *atsumaru) Details(ctx context.Context, ref sourcekit.Ref) (sourcekit.Details, error) {
	id := atsumaruMangaID(ref)
	if id == "" {
		return sourcekit.Details{}, fmt.Errorf("%q is not a manga url", ref.URL)
	}
	p, err := a.page(ctx, id)
	if err != nil {
		return sourcekit.Details{}, err
	}
	d := sourcekit.Details{
		Manga:       sourcekit.Manga{URL: "/manga/" + p.ID, ID: p.ID, Title: atsumaruTitle(p.Title, p.EnglishTitle), CoverURL: a.image(p.Poster.Image)},
		Description: strings.TrimSpace(p.Synopsis), Status: atsumaruStatus(p.Status), WebURL: a.site + "/manga/" + p.ID,
	}
	// Only expose an actual count. Fractional values are chapter numbers rather
	// than counts; the chapter listing fetched alongside details is authoritative.
	if p.TotalChapterCount > 0 && p.TotalChapterCount == float64(int(p.TotalChapterCount)) {
		n := p.TotalChapterCount
		count := int(n)
		d.Chapters = &count
	}
	var authors, artists []string
	for _, x := range p.Authors {
		if strings.EqualFold(x.Type, "Artist") {
			artists = append(artists, x.Name)
			continue
		}
		authors = append(authors, x.Name)
	}
	d.Author, d.Artist = strings.Join(authors, ", "), strings.Join(artists, ", ")
	for _, g := range p.Genres {
		d.Genres = append(d.Genres, g.Name)
	}
	return d, nil
}

func atsumaruStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ongoing", "releasing":
		return sourcekit.StatusOngoing
	case "completed", "finished":
		return sourcekit.StatusCompleted
	case "hiatus", "on hiatus":
		return sourcekit.StatusHiatus
	case "cancelled", "canceled", "discontinued":
		return sourcekit.StatusCancelled
	}
	return sourcekit.StatusUnknown
}

func (a *atsumaru) Chapters(ctx context.Context, ref sourcekit.Ref) ([]sourcekit.Chapter, error) {
	id := atsumaruMangaID(ref)
	if id == "" {
		return nil, fmt.Errorf("%q is not a manga url", ref.URL)
	}
	var out struct {
		Chapters []struct {
			ID                string  `json:"id"`
			Title             string  `json:"title"`
			Number            float64 `json:"number"`
			CreatedAt         int64   `json:"createdAt"`
			ScanlationMangaID string  `json:"scanlationMangaId"`
		} `json:"chapters"`
	}
	req := sourcekit.Request{URL: a.site + "/api/manga/allChapters", Query: url.Values{"mangaId": {id}}, Headers: a.headers("")}
	if err := a.c.JSON(ctx, req, &out); err != nil {
		return nil, err
	}
	if len(out.Chapters) == 0 {
		return nil, fmt.Errorf("%w: no chapters listed", sourcekit.ErrNotFound)
	}
	// the chapter list names its scanlators by id only; the manga page has
	// the names, and is one request whatever the chapter count
	names := map[string]string{}
	if p, err := a.page(ctx, id); err == nil {
		for _, s := range p.Scanlators {
			names[s.ID] = s.Name
		}
	}
	chapters := make([]sourcekit.Chapter, 0, len(out.Chapters))
	for _, c := range out.Chapters {
		if c.ID == "" {
			continue
		}
		ch := sourcekit.Chapter{URL: "/read/" + id + "/" + c.ID, ID: c.ID, Name: strings.TrimSpace(c.Title),
			Number: c.Number, Scanlator: names[c.ScanlationMangaID], WebURL: a.site + "/read/" + id + "/" + c.ID}
		if ch.Number == 0 {
			ch.Number = -1
		}
		if c.CreatedAt > 0 {
			t := time.UnixMilli(c.CreatedAt).UTC()
			ch.UploadedAt = &t
		}
		chapters = append(chapters, ch)
	}
	return chapters, nil
}

func (a *atsumaru) Pages(ctx context.Context, ch sourcekit.PageRef) ([]sourcekit.PageImage, error) {
	mangaID, chapterID := atsumaruChapterIDs(ch)
	if mangaID == "" || chapterID == "" {
		return nil, fmt.Errorf("%q is not a chapter url", ch.URL)
	}
	var out struct {
		ReadChapter struct {
			Pages []struct {
				Image string `json:"image"`
			} `json:"pages"`
		} `json:"readChapter"`
	}
	req := sourcekit.Request{URL: a.site + "/api/read/chapter", Headers: a.headers(a.site + "/read/" + mangaID + "/" + chapterID),
		Query: url.Values{"mangaId": {mangaID}, "chapterId": {chapterID}}}
	if err := a.c.JSON(ctx, req, &out); err != nil {
		return nil, err
	}
	var pages []sourcekit.PageImage
	for _, p := range out.ReadChapter.Pages {
		u := a.image(p.Image)
		if u == "" {
			continue
		}
		pages = append(pages, sourcekit.PageImage{Index: len(pages), URL: u,
			Headers: map[string]string{"Referer": a.site + "/", "Accept": "image/avif,image/webp,*/*"}})
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: the chapter has no pages", sourcekit.ErrNotFound)
	}
	return pages, nil
}

// image turns what the API reports into a CDN address. Paths come with and
// without the "/static/" prefix depending on the endpoint, and a few are
// already absolute.
func (a *atsumaru) image(raw string) string {
	s := strings.TrimSpace(raw)
	switch {
	case s == "":
		return ""
	case strings.HasPrefix(s, "http://"), strings.HasPrefix(s, "https://"):
		return s
	case strings.HasPrefix(s, "//"):
		return "https:" + s
	}
	return a.cdn + "/static/" + strings.TrimPrefix(strings.TrimPrefix(s, "/"), "static/")
}

func (a *atsumaru) headers(referer string) map[string]string {
	if referer == "" {
		referer = a.site + "/"
	}
	return map[string]string{"Referer": referer, "Accept": "*/*"}
}

// atsumaruTitle prefers the English title the site carries for a manga.
func atsumaruTitle(title, english string) string {
	if e := strings.TrimSpace(english); e != "" {
		return e
	}
	return strings.TrimSpace(title)
}

// atsumaruMangaID reads the id out of "/manga/<id>", falling back to the one
// stored with the link.
func atsumaruMangaID(ref sourcekit.Ref) string {
	parts := strings.Split(strings.Trim(sourcekit.Path(ref.URL), "/"), "/")
	for i, p := range parts {
		if (p == "manga" || p == "read") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ref.ID
}

// atsumaruChapterIDs reads "(manga, chapter)" out of "/read/<manga>/<chapter>".
func atsumaruChapterIDs(ch sourcekit.PageRef) (manga, chapter string) {
	parts := strings.Split(strings.Trim(sourcekit.Path(ch.URL), "/"), "/")
	for i, p := range parts {
		if p == "read" && i+2 < len(parts) {
			return parts[i+1], parts[i+2]
		}
	}
	return atsumaruMangaID(ch.Manga), ch.ID
}
