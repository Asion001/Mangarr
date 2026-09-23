package sites

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// Manga Ball (mangaball.net) is a Laravel site: its lists and chapter lists
// come from a form-posting API guarded by the page's CSRF token, while the
// manga and chapter pages are plain HTML.
//
// It is one catalog per language, with the ids Keiyoushi's "Manga Ball"
// sources have.
var mballLangs = []string{
	"ar", "bg", "bn", "ca", "cs", "da", "de", "el", "en", "es", "fa", "fi", "fr", "he", "hi", "hu",
	"id", "it", "is", "ja", "ko", "kn", "ml", "ms", "ne", "nl", "no", "pl", "pt-BR", "ro", "ru", "sk",
	"sl", "sq", "sr", "sv", "ta", "th", "tr", "uk", "vi", "zh",
}

// mballSiteLangs are the site's codes for a language where they differ from
// Keiyoushi's; a catalog asks for every regional variant ("es" also takes
// "es-mx", "es-ar", ...), as the extension does.
var mballSiteLangs = map[string][]string{
	"ca":    {"ca", "ca-ad", "ca-es", "ca-fr", "ca-it", "ca-pt"},
	"es":    {"es", "es-ar", "es-mx", "es-es", "es-la", "es-419"},
	"it":    {"it", "it-it"},
	"is":    {"ib", "ib-is", "is"},
	"ja":    {"jp"},
	"ko":    {"kr"},
	"kn":    {"kn", "kn-in", "kn-my", "kn-sg", "kn-tw"},
	"ml":    {"ml", "ml-in", "ml-my", "ml-sg", "ml-tw"},
	"nl":    {"nl", "nl-be"},
	"pt-BR": {"pt-br", "pt-pt"},
	"sr":    {"sr", "sr-cyrl"},
	"th":    {"th", "th-hk", "th-kh", "th-la", "th-my", "th-sg"},
	"zh":    {"zh", "zh-cn", "zh-hk", "zh-mo", "zh-sg", "zh-tw"},
}

const mballSite = "https://mangaball.net"

func init() {
	sourcekit.RegisterLangs("Manga Ball", 1, mballLangs, func(d sourcekit.Deps, lang string) sourcekit.Site {
		return &mangaball{c: d.Client, base: mballSite, lang: lang, adult: true}
	})
}

type mangaball struct {
	c *sourcekit.Client
	// base is the site (tests point it at a recorded copy).
	base string
	// lang is the catalog's language as Keiyoushi writes it.
	lang string
	// adult shows 18+ titles (the site's own cookie switch).
	adult bool

	mu   sync.Mutex
	csrf string
}

func (m *mangaball) Info() sourcekit.Info {
	return sourcekit.Info{ID: sourcekit.KeiyoushiID("Manga Ball", m.lang, 1), Name: "Manga Ball", Lang: m.lang,
		BaseURL: m.base, SupportsBrowse: true, IconURL: m.base + "/favicon.ico"}
}

// Politeness: the extension sets no limit of its own, so stay gentle.
func (m *mangaball) Politeness() sourcekit.Politeness {
	return sourcekit.Politeness{RequestsPerMinute: 60, MaxConcurrent: 2}
}

func (m *mangaball) Options() []sourcekit.Option {
	return []sourcekit.Option{{Key: "adult", Title: "Show 18+ titles", Type: "switch", Value: m.adult,
		Help: "On by default, as in the extension."}}
}

func (m *mangaball) SetOption(key string, value any) error {
	if key != "adult" {
		return fmt.Errorf("%w: no option %q", sourcekit.ErrUnsupported, key)
	}
	switch v := value.(type) {
	case bool:
		m.adult = v
	case string:
		m.adult = v == "true" || v == "1"
	default:
		return fmt.Errorf("%v is not a switch", value)
	}
	return nil
}

// siteLangs are the site's codes for the catalog's language.
func (m *mangaball) siteLangs() []string {
	if l, ok := mballSiteLangs[m.lang]; ok {
		return l
	}
	return []string{m.lang}
}

func (m *mangaball) headers() map[string]string {
	return map[string]string{"Referer": m.base + "/", "Origin": m.base,
		"Cookie": "show18PlusContent=" + strconv.FormatBool(m.adult)}
}

// ---- the CSRF-guarded API ---------------------------------------------------

// token is the page's CSRF token, read from the home page the first time.
func (m *mangaball) token(ctx context.Context) (string, error) {
	m.mu.Lock()
	t := m.csrf
	m.mu.Unlock()
	if t != "" {
		return t, nil
	}
	doc, err := m.c.Document(ctx, sourcekit.Request{URL: m.base, Headers: m.headers()})
	if err != nil {
		return "", err
	}
	m.keepToken(doc)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.csrf == "" {
		return "", fmt.Errorf("mangaball: no CSRF token on the home page")
	}
	return m.csrf, nil
}

// keepToken remembers the CSRF token of any page the site served.
func (m *mangaball) keepToken(doc *goquery.Document) {
	if t := strings.TrimSpace(doc.Find("meta[name=csrf-token]").First().AttrOr("content", "")); t != "" {
		m.mu.Lock()
		m.csrf = t
		m.mu.Unlock()
	}
}

// api posts a form to the API; a 403 means the token went stale, so it is
// fetched again and the request retried once.
func (m *mangaball) api(ctx context.Context, path string, form url.Values, out any) error {
	for attempt := 0; ; attempt++ {
		t, err := m.token(ctx)
		if err != nil {
			return err
		}
		h := m.headers()
		h["X-Requested-With"] = "XMLHttpRequest"
		h["X-CSRF-TOKEN"] = t
		h["Content-Type"] = "application/x-www-form-urlencoded"
		err = m.c.JSON(ctx, sourcekit.Request{Method: "POST", URL: m.base + path, Headers: h, Body: []byte(form.Encode())}, out)
		var se *sourcekit.StatusError
		if err != nil && attempt == 0 && errorsAs(err, &se) && se.Code == 403 {
			m.mu.Lock()
			m.csrf = ""
			m.mu.Unlock()
			continue
		}
		return err
	}
}

// ---- lists ------------------------------------------------------------------

const (
	mballSortLatest = "updated_chapters_desc"
	mballSortViews  = "views_desc"
)

func (m *mangaball) Search(ctx context.Context, query string, page int) (sourcekit.Results, error) {
	if page < 1 {
		page = 1
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return m.advanced(ctx, page, "", mballSortLatest)
	}
	// the site's quick search answers the first page; the advanced search
	// carries on from there
	if page == 1 {
		return m.quickSearch(ctx, query)
	}
	return m.advanced(ctx, page-1, query, mballSortLatest)
}

func (m *mangaball) Popular(ctx context.Context, page int) (sourcekit.Results, error) {
	return m.advanced(ctx, max(page, 1), "", mballSortViews)
}

func (m *mangaball) Latest(ctx context.Context, page int) (sourcekit.Results, error) {
	return m.advanced(ctx, max(page, 1), "", mballSortLatest)
}

func (m *mangaball) quickSearch(ctx context.Context, query string) (sourcekit.Results, error) {
	var out struct {
		Data struct {
			Manga []struct {
				Title string `json:"title"`
				Img   string `json:"img"`
				URL   string `json:"url"`
			} `json:"manga"`
		} `json:"data"`
	}
	if err := m.api(ctx, "/api/v1/smart-search/search/", url.Values{"search_input": {query}}, &out); err != nil {
		return sourcekit.Results{}, err
	}
	var res sourcekit.Results
	for _, x := range out.Data.Manga {
		slug := mballSlug(x.URL)
		if slug == "" || res.Has(slug) {
			continue
		}
		res.Mangas = append(res.Mangas, sourcekit.Manga{URL: slug, Title: strings.TrimSpace(x.Title), CoverURL: sourcekit.Abs(m.base, x.Img)})
	}
	res.HasNext = true // the advanced search has more
	return res, nil
}

func (m *mangaball) advanced(ctx context.Context, page int, query, sortBy string) (sourcekit.Results, error) {
	form := url.Values{}
	form.Set("search_input", query)
	form.Set("filters[sort]", sortBy)
	form.Set("filters[page]", strconv.Itoa(page))
	form.Set("filters[tag_included_mode]", "and")
	form.Set("filters[tag_excluded_mode]", "and")
	form.Set("filters[contentRating]", "any")
	form.Set("filters[demographic]", "any")
	form.Set("filters[person]", "any")
	form.Set("filters[publicationYear]", "")
	form.Set("filters[publicationStatus]", "any")
	for _, l := range m.siteLangs() {
		form.Add("filters[translatedLanguage][]", l)
	}
	var out struct {
		Data []struct {
			URL   string `json:"url"`
			Name  string `json:"name"`
			Cover string `json:"cover"`
		} `json:"data"`
		Pagination struct {
			CurrentPage int `json:"current_page"`
			LastPage    int `json:"last_page"`
		} `json:"pagination"`
	}
	if err := m.api(ctx, "/api/v1/title/search-advanced/", form, &out); err != nil {
		return sourcekit.Results{}, err
	}
	var res sourcekit.Results
	for _, x := range out.Data {
		slug := mballSlug(x.URL)
		if slug == "" || res.Has(slug) {
			continue
		}
		res.Mangas = append(res.Mangas, sourcekit.Manga{URL: slug, Title: strings.TrimSpace(x.Name), CoverURL: sourcekit.Abs(m.base, x.Cover)})
	}
	res.HasNext = out.Pagination.CurrentPage < out.Pagination.LastPage
	return res, nil
}

// mballSlug is what Keiyoushi stores as a manga's url: the slug in
// "/title-detail/<slug>/" (a bare slug is taken as it is).
func mballSlug(raw string) string {
	parts := strings.Split(strings.Trim(sourcekit.Path(raw), "/"), "/")
	switch {
	case len(parts) >= 2 && (parts[0] == "title-detail" || parts[0] == "chapter-detail"):
		return parts[1]
	case len(parts) == 1:
		return parts[0]
	}
	return ""
}

// ---- one manga --------------------------------------------------------------

func (m *mangaball) Details(ctx context.Context, ref sourcekit.Ref) (sourcekit.Details, error) {
	slug := mballSlug(ref.URL)
	if slug == "" {
		return sourcekit.Details{}, fmt.Errorf("%q is not a manga url", ref.URL)
	}
	page := m.base + "/title-detail/" + slug + "/"
	doc, err := m.c.Document(ctx, sourcekit.Request{URL: page, Headers: m.headers()})
	if err != nil {
		return sourcekit.Details{}, notFound(err)
	}
	m.keepToken(doc)
	title := mballOwnText(doc.Find("#comicDetail h6").First())
	if title == "" {
		return sourcekit.Details{}, fmt.Errorf("%w: no title on %s", sourcekit.ErrNotFound, page)
	}
	d := sourcekit.Details{Manga: sourcekit.Manga{URL: slug, Title: title}, WebURL: page, Status: sourcekit.StatusUnknown}
	if src, ok := doc.Find("img.featured-cover").First().Attr("src"); ok {
		d.CoverURL = sourcekit.Abs(page, src)
	}
	if flag := doc.Find("#featuredComicsCarousel img[src*='/flags/']").First().AttrOr("src", ""); flag != "" {
		switch {
		case strings.Contains(flag, "jp"):
			d.Genres = append(d.Genres, "Manga")
		case strings.Contains(flag, "kr"):
			d.Genres = append(d.Genres, "Manhwa")
		case strings.Contains(flag, "cn"):
			d.Genres = append(d.Genres, "Manhua")
		}
	}
	doc.Find("#comicDetail span[data-tag-id]").Each(func(_ int, s *goquery.Selection) {
		if g := mballOwnText(s); g != "" {
			d.Genres = append(d.Genres, g)
		}
	})
	var authors []string
	doc.Find("#comicDetail span[data-person-id]").Each(func(_ int, s *goquery.Selection) {
		if a := text(s); a != "" {
			authors = append(authors, a)
		}
	})
	d.Author = strings.Join(authors, ", ")
	d.Description = strings.TrimSpace(doc.Find("#descriptionContent p").First().Text())
	switch text(doc.Find("span.badge-status").First()) {
	case "Ongoing":
		d.Status = sourcekit.StatusOngoing
	case "Completed":
		d.Status = sourcekit.StatusCompleted
	case "Hiatus":
		d.Status = sourcekit.StatusHiatus
	case "Cancelled":
		d.Status = sourcekit.StatusCancelled
	}
	return d, nil
}

// mballOwnText is an element's own text, without its children's.
func mballOwnText(s *goquery.Selection) string {
	var b strings.Builder
	for _, n := range s.Nodes {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				b.WriteString(c.Data)
			}
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// mballGroupID is a group the site hosts itself; any other id names the site
// the chapter was taken from.
var mballGroupID = regexp.MustCompile(`^[a-z0-9]{24}$`)

func (m *mangaball) Chapters(ctx context.Context, ref sourcekit.Ref) ([]sourcekit.Chapter, error) {
	slug := mballSlug(ref.URL)
	if slug == "" {
		return nil, fmt.Errorf("%q is not a manga url", ref.URL)
	}
	id := slug[strings.LastIndex(slug, "-")+1:]
	var out struct {
		Chapters []struct {
			Number       float64 `json:"number_float"`
			Translations []struct {
				ID       string  `json:"id"`
				Name     string  `json:"name"`
				Language string  `json:"language"`
				Date     string  `json:"date"`
				Volume   float64 `json:"volume"`
				Group    struct {
					ID   string `json:"_id"`
					Name string `json:"name"`
				} `json:"group"`
			} `json:"translations"`
		} `json:"ALL_CHAPTERS"`
	}
	if err := m.api(ctx, "/api/v1/chapter/chapter-listing-by-title-id/", url.Values{"title_id": {id}}, &out); err != nil {
		return nil, notFound(err)
	}
	wanted := map[string]bool{}
	for _, l := range m.siteLangs() {
		wanted[l] = true
	}
	var chapters []sourcekit.Chapter
	for _, c := range out.Chapters {
		number := strconv.FormatFloat(c.Number, 'f', -1, 32)
		for _, t := range c.Translations {
			if !wanted[t.Language] || t.ID == "" {
				continue
			}
			var name strings.Builder
			if t.Volume > 0 {
				name.WriteString("Vol. " + strconv.FormatFloat(t.Volume, 'f', -1, 32) + " ")
			}
			if strings.Contains(t.Name, number) {
				name.WriteString(strings.TrimSpace(t.Name))
			} else {
				name.WriteString("Ch. " + number + " " + strings.TrimSpace(t.Name))
			}
			scanlator := t.Group.Name
			if !mballGroupID.MatchString(t.Group.ID) {
				scanlator += " (" + t.Group.ID + ")"
			}
			ch := sourcekit.Chapter{URL: t.ID, ID: t.ID, Name: strings.TrimSpace(name.String()), Number: c.Number,
				Scanlator: strings.TrimSpace(scanlator), WebURL: m.base + "/chapter-detail/" + t.ID + "/"}
			if at, err := time.Parse("2006-01-02 15:04:05", t.Date); err == nil {
				ch.UploadedAt = &at
			}
			chapters = append(chapters, ch)
		}
	}
	return chapters, nil
}

// mballImages is the list of pages a chapter's page script carries.
var mballImages = regexp.MustCompile("const\\s+chapterImages\\s*=\\s*JSON\\.parse\\(`([^`]+)`\\)")

func (m *mangaball) Pages(ctx context.Context, ch sourcekit.PageRef) ([]sourcekit.PageImage, error) {
	id := mballSlug(ch.URL)
	if id == "" {
		id = ch.ID
	}
	if id == "" {
		return nil, fmt.Errorf("%q is not a chapter url", ch.URL)
	}
	page := m.base + "/chapter-detail/" + id + "/"
	doc, err := m.c.Document(ctx, sourcekit.Request{URL: page, Headers: m.headers()})
	if err != nil {
		return nil, notFound(err)
	}
	m.keepToken(doc)
	var scripts []string
	doc.Find("script").Each(func(_ int, s *goquery.Selection) {
		if body := s.Text(); strings.Contains(body, "chapterImages") {
			scripts = append(scripts, body)
		}
	})
	match := mballImages.FindStringSubmatch(strings.Join(scripts, ";"))
	if match == nil {
		return nil, fmt.Errorf("%w: no page list on %s", sourcekit.ErrNotFound, page)
	}
	var images []string
	if err := json.Unmarshal([]byte(match[1]), &images); err != nil {
		return nil, fmt.Errorf("%s: page list: %w", page, err)
	}
	var pages []sourcekit.PageImage
	for _, img := range images {
		if img = strings.TrimSpace(img); img == "" {
			continue
		}
		pages = append(pages, sourcekit.PageImage{Index: len(pages), URL: sourcekit.Abs(page, img),
			Headers: map[string]string{"Referer": m.base + "/"}})
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: the chapter has no pages", sourcekit.ErrNotFound)
	}
	return pages, nil
}
