package sites

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// MadTheme is the site engine a family of manga sites share (Keiyoushi's
// "madtheme"): server-rendered search and manga pages, a chapter list served
// by a separate, heavily rate-limited endpoint, and page images listed either
// as <img> tags or in a script. One engine serves every site on the theme;
// a site is a madTheme with its own name, address and quirks.

// madSites are the sites on the theme. The ids are the Keiyoushi ones (set
// explicitly in each extension's build file), so Mihon backups link here.
var madSites = []madTheme{
	// KaliScan also answers on kaliscan.me, kaliscan.io and mgjinx.com.
	{id: "7660637864742395387", name: "KaliScan", base: "https://kaliscan.com", legacyAPI: true},
}

func init() {
	for _, s := range madSites {
		sourcekit.Register(s.id, func(d sourcekit.Deps) sourcekit.Site {
			m := s
			m.c = d.Client
			m.chapterEvery = madChapterEvery
			m.gate = &madGate{}
			return &m
		})
	}
}

// madChapterEvery is how often the theme's chapter endpoint may be asked:
// the extension keeps a separate client limited to one call in 12 seconds.
const madChapterEvery = 12 * time.Second

type madTheme struct {
	c    *sourcekit.Client
	id   string
	name string
	// base is the site's address (tests point it at a recorded copy).
	base string
	// legacyAPI: chapters come from /service/backend/chaplist/ (older
	// installs of the theme) rather than /api/manga/<id>/chapters.
	legacyAPI bool
	// slugSearch: the chapter API wants the manga's slug, not its number.
	slugSearch bool
	// nsfw marks sites that are adult-only.
	nsfw bool

	chapterEvery time.Duration
	gate         *madGate
}

// madGate spaces out calls to the chapter endpoint.
type madGate struct {
	mu   sync.Mutex
	last time.Time
}

func (g *madGate) wait(ctx context.Context, every time.Duration) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if d := every - time.Since(g.last); d > 0 && !g.last.IsZero() {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	g.last = time.Now()
	return nil
}

func (m *madTheme) Info() sourcekit.Info {
	return sourcekit.Info{ID: m.id, Name: m.name, Lang: "en", BaseURL: m.base, NSFW: m.nsfw, SupportsBrowse: true,
		IconURL: m.base + "/favicon.ico"}
}

// Politeness: the extension allows one request a second.
func (m *madTheme) Politeness() sourcekit.Politeness {
	return sourcekit.Politeness{RequestsPerMinute: 60, MaxConcurrent: 1}
}

func (m *madTheme) headers() map[string]string {
	return map[string]string{"Referer": m.base + "/"}
}

// ---- lists ------------------------------------------------------------------

func (m *madTheme) Search(ctx context.Context, query string, page int) (sourcekit.Results, error) {
	// the extension's default filters: every status, most viewed first
	return m.list(ctx, page, url.Values{"q": {strings.TrimSpace(query)}, "status": {"all"}, "sort": {"views"}})
}

func (m *madTheme) Popular(ctx context.Context, page int) (sourcekit.Results, error) {
	return m.list(ctx, page, url.Values{"q": {""}, "sort": {"views"}})
}

func (m *madTheme) Latest(ctx context.Context, page int) (sourcekit.Results, error) {
	return m.list(ctx, page, url.Values{"q": {""}, "sort": {"updated_at"}})
}

func (m *madTheme) list(ctx context.Context, page int, q url.Values) (sourcekit.Results, error) {
	if page < 1 {
		page = 1
	}
	q.Set("page", strconv.Itoa(page))
	doc, err := m.c.Document(ctx, sourcekit.Request{URL: m.base + "/search", Query: q, Headers: m.headers()})
	if err != nil {
		return sourcekit.Results{}, err
	}
	var res sourcekit.Results
	doc.Find(".book-detailed-item").Each(func(_ int, el *goquery.Selection) {
		a := el.Find("a").First()
		href, ok := a.Attr("href")
		if !ok {
			return
		}
		path := sourcekit.Path(sourcekit.Abs(m.base, href))
		if path == "" || res.Has(path) {
			return
		}
		res.Mangas = append(res.Mangas, sourcekit.Manga{URL: path, Title: strings.TrimSpace(a.AttrOr("title", "")),
			CoverURL: sourcekit.Abs(m.base, el.Find("img").First().AttrOr("data-src", ""))})
	})
	// only some sites show next/previous buttons, so the next page is the
	// link after the active one
	res.HasNext = doc.Find(".paginator > a.active + a:not([rel=next])").Length() > 0
	return res, nil
}

// ---- one manga --------------------------------------------------------------

func (m *madTheme) Details(ctx context.Context, ref sourcekit.Ref) (sourcekit.Details, error) {
	path := sourcekit.Path(ref.URL)
	doc, err := m.c.Document(ctx, sourcekit.Request{URL: m.base + path, Headers: m.headers()})
	if err != nil {
		return sourcekit.Details{}, notFound(err)
	}
	d := sourcekit.Details{Manga: sourcekit.Manga{URL: path, Title: text(doc.Find(".detail h1").First()),
		CoverURL: sourcekit.Abs(m.base, doc.Find("#cover img").First().AttrOr("data-src", ""))},
		WebURL: m.base + path}
	if d.Title == "" {
		return sourcekit.Details{}, fmt.Errorf("%w: %s has no title", sourcekit.ErrNotFound, path)
	}
	d.Author = strings.Join(madMeta(doc, "Authors"), ", ")
	d.Genres = madMeta(doc, "Genres")

	var alt []string
	for _, n := range strings.FieldsFunc(text(doc.Find(".detail h2").First()), func(r rune) bool { return r == ',' || r == ';' }) {
		if n = strings.TrimSpace(n); n != "" && n != d.Title {
			alt = append(alt, n)
		}
	}
	d.Description = madJoinText(doc.Find(".summary .content, .summary .content ~ p"))
	if len(alt) > 0 {
		d.Description += "\n\nAlt name(s): " + strings.Join(alt, ", ")
	}
	d.Description = strings.TrimSpace(d.Description)

	d.Status = sourcekit.StatusUnknown
	if s := madMeta(doc, "Status"); len(s) > 0 {
		switch strings.ToLower(s[0]) {
		case "ongoing":
			d.Status = sourcekit.StatusOngoing
		case "completed":
			d.Status = sourcekit.StatusCompleted
		case "on-hold":
			d.Status = sourcekit.StatusHiatus
		case "canceled":
			d.Status = sourcekit.StatusCancelled
		}
	}
	return d, nil
}

// madMeta reads the links after a ".meta > p > strong" label.
func madMeta(doc *goquery.Document, label string) []string {
	var out []string
	doc.Find(".detail .meta > p > strong:contains(" + label + ") ~ a").Each(func(_ int, a *goquery.Selection) {
		if v := strings.Trim(text(a), ", "); v != "" {
			out = append(out, v)
		}
	})
	return out
}

var (
	madMangaIDRe   = regexp.MustCompile(`/manga/(\d+)-`)
	madChapterIDRe = regexp.MustCompile(`chapterId\s*=\s*(\d+)`)
	madNumberRe    = regexp.MustCompile(`\d+`)
	madSlashesRe   = regexp.MustCompile(`/{2,}`)
)

func (m *madTheme) Chapters(ctx context.Context, ref sourcekit.Ref) ([]sourcekit.Chapter, error) {
	path := sourcekit.Path(ref.URL)
	var mangaID string
	if g := madMangaIDRe.FindStringSubmatch(path); g != nil {
		mangaID = g[1]
	}
	slug := path
	if i := strings.LastIndex(slug, "/"); i >= 0 {
		slug = slug[i+1:]
	}
	if i := strings.Index(slug, "?"); i >= 0 {
		slug = slug[:i]
	}

	var endpoint string
	var q url.Values
	switch {
	case m.legacyAPI && mangaID != "":
		title := strings.TrimSpace(ref.Title)
		if title == "" {
			// the endpoint wants the title too; the manga page has it
			if d, err := m.Details(ctx, ref); err == nil {
				title = d.Title
			}
		}
		endpoint, q = m.base+"/service/backend/chaplist/", url.Values{"manga_id": {mangaID}, "manga_name": {title}}
	case !m.legacyAPI && m.slugSearch && slug != "", !m.legacyAPI && !m.slugSearch && mangaID != "":
		endpoint, q = m.chapterAPI(mangaID, slug)
	default:
		// no id to ask the API with: read the list off the manga page
		return m.chaptersFromPage(ctx, path)
	}
	if err := m.gate.wait(ctx, m.chapterEvery); err != nil {
		return nil, err
	}
	doc, err := m.c.Document(ctx, sourcekit.Request{URL: endpoint, Query: q, Headers: m.headers()})
	if err != nil {
		return nil, notFound(err)
	}
	return m.chapterList(doc, nil)
}

// chapterAPI is where the non-legacy API lists a manga's chapters.
func (m *madTheme) chapterAPI(mangaID, slug string) (string, url.Values) {
	key := mangaID
	if m.slugSearch {
		key = slug
	}
	return m.base + "/api/manga/" + url.PathEscape(key) + "/chapters", url.Values{"source": {"detail"}}
}

// chaptersFromPage reads the chapters on the manga page and, when the page
// only shows the first few, fetches the rest from the API.
func (m *madTheme) chaptersFromPage(ctx context.Context, path string) ([]sourcekit.Chapter, error) {
	if err := m.gate.wait(ctx, m.chapterEvery); err != nil {
		return nil, err
	}
	doc, err := m.c.Document(ctx, sourcekit.Request{URL: m.base + path, Headers: m.headers()})
	if err != nil {
		return nil, notFound(err)
	}
	onPage := m.chapterRows(doc)
	if doc.Find("div#show-more-chapters > span").First().AttrOr("onclick", "") != "getChapters()" {
		return m.chapterList(nil, onPage)
	}
	var script string
	doc.Find("script").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if strings.Contains(s.Text(), "bookId") {
			script = s.Text()
			return false
		}
		return true
	})
	if script == "" {
		return nil, fmt.Errorf("%s: cannot find the script with the book id", path)
	}
	bookID := madBetween(script, "bookId = ", ";")
	bookSlug := madBetween(script, `bookSlug = "`, `";`)
	endpoint, q := m.chapterAPI(bookID, bookSlug)
	api, err := m.c.Document(ctx, sourcekit.Request{URL: endpoint, Query: q, Headers: m.headers()})
	if err != nil {
		return nil, err
	}
	fromAPI := m.chapterRows(api)
	// the page shows the newest chapters, the API all of them: keep the page's
	// up to where the API's begin
	inAPI := map[string]bool{}
	for _, c := range fromAPI {
		inAPI[c.URL] = true
	}
	cut := len(onPage)
	for i, c := range onPage {
		if inAPI[c.URL] {
			cut = i
			break
		}
	}
	return m.chapterList(nil, append(onPage[:cut:cut], fromAPI...))
}

// chapterList drops repeated chapters (and parses doc when given).
func (m *madTheme) chapterList(doc *goquery.Document, rows []sourcekit.Chapter) ([]sourcekit.Chapter, error) {
	if doc != nil {
		rows = m.chapterRows(doc)
	}
	seen := map[string]bool{}
	out := make([]sourcekit.Chapter, 0, len(rows))
	for _, c := range rows {
		if !seen[c.URL] {
			seen[c.URL] = true
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no chapters listed", sourcekit.ErrNotFound)
	}
	return out, nil
}

func (m *madTheme) chapterRows(doc *goquery.Document) []sourcekit.Chapter {
	var out []sourcekit.Chapter
	now := time.Now()
	doc.Find("#chapter-list > li").Each(func(_ int, li *goquery.Selection) {
		href, ok := li.Find("a").First().Attr("href")
		if !ok {
			return
		}
		raw := sourcekit.Abs(m.base, href)
		ch := sourcekit.Chapter{URL: raw, WebURL: raw, Name: text(li.Find(".chapter-title").First())}
		// chapters hosted elsewhere keep their full address
		if strings.HasPrefix(raw, m.base) {
			ch.URL = madSlashesRe.ReplaceAllString(strings.TrimPrefix(raw, m.base), "/")
			ch.WebURL = m.base + ch.URL
		}
		ch.Number = chapterNumber(ch.Name)
		if up := li.Find(".chapter-update").First(); up.Length() > 0 {
			ch.UploadedAt = madDate(text(up), now)
		}
		out = append(out, ch)
	})
	return out
}

// madDate reads "Mar 05, 2024" or "3 days ago".
func madDate(s string, now time.Time) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if strings.Contains(s, " ago") {
		n, err := strconv.Atoi(madNumberRe.FindString(s))
		if err != nil {
			return nil
		}
		var t time.Time
		switch {
		case strings.Contains(s, "year"):
			t = now.AddDate(-n, 0, 0)
		case strings.Contains(s, "month"):
			t = now.AddDate(0, -n, 0)
		case strings.Contains(s, "day"):
			t = now.AddDate(0, 0, -n)
		case strings.Contains(s, "hour"):
			t = now.Add(-time.Duration(n) * time.Hour)
		case strings.Contains(s, "minute"):
			t = now.Add(-time.Duration(n) * time.Minute)
		case strings.Contains(s, "second"):
			t = now.Add(-time.Duration(n) * time.Second)
		default:
			return nil
		}
		return &t
	}
	t, err := time.Parse("Jan 2, 2006", s)
	if err != nil {
		return nil
	}
	return &t
}

// ---- pages ------------------------------------------------------------------

func (m *madTheme) Pages(ctx context.Context, ch sourcekit.PageRef) ([]sourcekit.PageImage, error) {
	location := ch.URL
	if !strings.HasPrefix(location, "http://") && !strings.HasPrefix(location, "https://") {
		location = m.base + sourcekit.Path(location)
	}
	body, err := m.c.Do(ctx, sourcekit.Request{URL: location, Headers: m.headers()})
	if err != nil {
		return nil, notFound(err)
	}
	html := string(body)
	// sites that load their images from a chapter server say so in the page
	if madMangaIDRe.MatchString(location) {
		if g := madChapterIDRe.FindStringSubmatch(html); g != nil {
			server, err := m.c.Do(ctx, sourcekit.Request{URL: m.base + "/service/backend/chapterServer/",
				Query: url.Values{"server_id": {"1"}, "chapter_id": {g[1]}}, Headers: m.headers()})
			if err != nil {
				return nil, err
			}
			html = string(server)
		}
	}
	urls, err := madPageURLs(html, location)
	if err != nil {
		return nil, err
	}
	pages := make([]sourcekit.PageImage, 0, len(urls))
	for _, u := range urls {
		if u == "" {
			continue
		}
		pages = append(pages, sourcekit.PageImage{Index: len(pages), URL: u, Headers: m.headers()})
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: the chapter has no pages", sourcekit.ErrNotFound)
	}
	return pages, nil
}

// madPageURLs lists a chapter's images from its html: a main server plus
// paths in a script, or <img> tags (or the script, when a site lazy-loads all
// but the first few tags).
func madPageURLs(html, location string) ([]string, error) {
	if strings.Contains(html, `var mainServer = "`) {
		server := madBetween(html, `var mainServer = "`, `"`)
		if strings.HasPrefix(server, "//") {
			server = "https:" + server
		}
		var out []string
		for _, p := range strings.Split(madBetween(html, "var chapImages = '", "'"), ",") {
			out = append(out, server+p)
		}
		return out, nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}
	var tags []string
	doc.Find("#chapter-images img, .chapter-image[data-src]").Each(func(_ int, img *goquery.Selection) {
		tags = append(tags, madImage(img, location))
	})
	if strings.Contains(html, "var chapImages = '") {
		fromJS := strings.Split(madBetween(html, "var chapImages = '", "'"), ",")
		absolute := true
		for _, u := range fromJS {
			if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
				absolute = false
			}
		}
		if absolute && len(tags) < len(fromJS) {
			return fromJS, nil
		}
	}
	return tags, nil
}

// madImage is an image tag's address. A tag names a fallback in its onerror
// handler; the extension retries with it when the first server fails and
// skips the s20 server, known broken, altogether. mangarr has no retry
// address per page, so only the skip is kept.
func madImage(img *goquery.Selection, location string) string {
	src := sourcekit.Abs(location, img.AttrOr("data-src", ""))
	raw := madBetween(img.AttrOr("onerror", ""), "this.src='", "'")
	fallback := ""
	switch {
	case strings.HasPrefix(raw, "https://"), strings.HasPrefix(raw, "http://"):
		fallback = raw
	case strings.HasPrefix(raw, "//"):
		fallback = "https:" + raw
	}
	if fallback != "" && strings.Contains(src, "://s20.") {
		return fallback
	}
	return src
}

// madJoinText is jsoup's Elements.text(): each element's text, joined by
// spaces.
func madJoinText(sel *goquery.Selection) string {
	var parts []string
	sel.Each(func(_ int, s *goquery.Selection) {
		if t := text(s); t != "" {
			parts = append(parts, t)
		}
	})
	return strings.Join(parts, " ")
}

// madBetween is Kotlin's substringAfter(start).substringBefore(end): the
// text after start up to end (the rest when end is missing, "" when start is).
func madBetween(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	s = s[i+len(start):]
	if j := strings.Index(s, end); j >= 0 {
		return s[:j]
	}
	return s
}
