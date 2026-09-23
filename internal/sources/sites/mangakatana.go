package sites

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// MangaKatana has no API, so this reads its pages. A manga's page carries
// its chapters too, and a chapter's page lists its images in a script. The
// id is the one Mihon's "MangaKatana" extension has.
var mangakatanaID = sourcekit.KeiyoushiID("MangaKatana", "en", 1)

const mangakatanaSite = "https://mangakatana.com"

// mangakatanaServers are the image servers a reader can pick, as the site's
// "sv" parameter.
var mangakatanaServers = []sourcekit.Choice{{Value: "", Label: "Server 1"}, {Value: "mk", Label: "Server 2"}, {Value: "3", Label: "Server 3"}}

func init() {
	sourcekit.Register(mangakatanaID, func(d sourcekit.Deps) sourcekit.Site {
		return &mangakatana{c: d.Client, base: mangakatanaSite}
	})
}

type mangakatana struct {
	c *sourcekit.Client
	// base is the address to talk to (tests point it at a recorded copy).
	base string
	// server is the image server pages come from ("" is the site's default).
	server string
}

func (k *mangakatana) Info() sourcekit.Info {
	return sourcekit.Info{ID: mangakatanaID, Name: "MangaKatana", Lang: "en", BaseURL: k.base, SupportsBrowse: true,
		IconURL: k.base + "/favicon.ico"}
}

// Politeness: a scraped site with no limit in the extension, so stay gentle.
func (k *mangakatana) Politeness() sourcekit.Politeness {
	return sourcekit.Politeness{RequestsPerMinute: 60, MaxConcurrent: 2}
}

func (k *mangakatana) Options() []sourcekit.Option {
	return []sourcekit.Option{{Key: "server", Title: "Image server", Type: "select", Value: k.server, Choices: mangakatanaServers,
		Help: "Try another when pages fail to load."}}
}

func (k *mangakatana) SetOption(key string, value any) error {
	if key != "server" {
		return fmt.Errorf("%w: no option %q", sourcekit.ErrUnsupported, key)
	}
	v, ok := value.(string)
	if !ok {
		return fmt.Errorf("%v is not a server", value)
	}
	for _, c := range mangakatanaServers {
		if c.Value == v {
			k.server = v
			return nil
		}
	}
	return fmt.Errorf("unknown server %q", v)
}

// ---- search -----------------------------------------------------------------

// Search asks the site's search by title. When a single manga matches, the
// site sends its page instead of a list.
func (k *mangakatana) Search(ctx context.Context, query string, page int) (sourcekit.Results, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return k.Popular(ctx, page)
	}
	if page < 1 {
		page = 1
	}
	values := url.Values{"search": {q}, "search_by": {"book_name"}}
	doc, err := k.c.Document(ctx, sourcekit.Request{URL: k.base + "/page/" + strconv.Itoa(page), Query: values, Headers: k.headers()})
	if err != nil {
		return sourcekit.Results{}, err
	}
	if doc.Find("div#book_list").Length() == 0 && doc.Find("h1.heading").Length() > 0 {
		m := sourcekit.Manga{URL: k.redirectedPath(doc), Title: text(doc.Find("h1.heading").First()), CoverURL: k.cover(doc)}
		if m.URL == "" {
			return sourcekit.Results{}, fmt.Errorf("the site sent a manga page without its address")
		}
		return sourcekit.Results{Mangas: []sourcekit.Manga{m}}, nil
	}
	return k.list(doc), nil
}

// Popular is the site's full list, which it sorts alphabetically.
func (k *mangakatana) Popular(ctx context.Context, page int) (sourcekit.Results, error) {
	return k.browse(ctx, "/manga/page/", page)
}

func (k *mangakatana) Latest(ctx context.Context, page int) (sourcekit.Results, error) {
	return k.browse(ctx, "/page/", page)
}

func (k *mangakatana) browse(ctx context.Context, path string, page int) (sourcekit.Results, error) {
	if page < 1 {
		page = 1
	}
	doc, err := k.c.Document(ctx, sourcekit.Request{URL: k.base + path + strconv.Itoa(page), Headers: k.headers()})
	if err != nil {
		return sourcekit.Results{}, err
	}
	return k.list(doc), nil
}

func (k *mangakatana) list(doc *goquery.Document) sourcekit.Results {
	var res sourcekit.Results
	doc.Find("div#book_list > div.item").Each(func(_ int, item *goquery.Selection) {
		a := item.Find("div.text > h3 > a").First()
		path := sourcekit.Path(sourcekit.Abs(k.base, a.AttrOr("href", "")))
		if path == "" || res.Has(path) {
			return
		}
		m := sourcekit.Manga{URL: path, Title: mangakatanaOwnText(a)}
		if src := item.Find("img").First().AttrOr("src", ""); src != "" {
			m.CoverURL = sourcekit.Abs(k.base, src)
		}
		res.Mangas = append(res.Mangas, m)
	})
	res.HasNext = doc.Find("a.next.page-numbers").Length() > 0
	return res
}

// redirectedPath is where a manga page the search redirected to lives: its
// canonical link, or the manga part of one of its chapter links.
func (k *mangakatana) redirectedPath(doc *goquery.Document) string {
	for _, sel := range []string{`link[rel="canonical"]`, `meta[property="og:url"]`} {
		s := doc.Find(sel).First()
		if u := s.AttrOr("href", s.AttrOr("content", "")); u != "" {
			if p := mangakatanaMangaPath(sourcekit.Path(sourcekit.Abs(k.base, u))); p != "" {
				return p
			}
		}
	}
	href := doc.Find("tr:has(.chapter) a").First().AttrOr("href", "")
	return mangakatanaMangaPath(sourcekit.Path(sourcekit.Abs(k.base, href)))
}

// mangakatanaMangaPath is "/manga/<slug>" out of a manga or chapter path.
func mangakatanaMangaPath(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) >= 2 && parts[0] == "manga" && parts[1] != "page" {
		return "/manga/" + parts[1]
	}
	return ""
}

// ---- one manga --------------------------------------------------------------

func (k *mangakatana) page(ctx context.Context, ref sourcekit.Ref) (*goquery.Document, string, error) {
	path := mangakatanaMangaPath(sourcekit.Path(ref.URL))
	if path == "" {
		return nil, "", fmt.Errorf("%q is not a manga url", ref.URL)
	}
	doc, err := k.c.Document(ctx, sourcekit.Request{URL: k.base + path, Headers: k.headers()})
	if err != nil {
		var se *sourcekit.StatusError
		if errorsAs(err, &se) && se.Code == 404 {
			return nil, "", fmt.Errorf("%w: %s", sourcekit.ErrNotFound, path)
		}
		return nil, "", err
	}
	return doc, path, nil
}

func (k *mangakatana) Details(ctx context.Context, ref sourcekit.Ref) (sourcekit.Details, error) {
	doc, path, err := k.page(ctx, ref)
	if err != nil {
		return sourcekit.Details{}, err
	}
	d := sourcekit.Details{Manga: sourcekit.Manga{URL: path, Title: text(doc.Find("h1.heading").First()), CoverURL: k.cover(doc)},
		WebURL: k.base + path, Status: sourcekit.StatusUnknown}
	if d.Title == "" {
		return sourcekit.Details{}, fmt.Errorf("%w: no manga at %s", sourcekit.ErrNotFound, path)
	}
	var authors []string
	doc.Find(".author").Each(func(_ int, s *goquery.Selection) {
		if v := text(s); v != "" {
			authors = append(authors, v)
		}
	})
	d.Author = strings.Join(authors, ", ")
	var summary []string
	doc.Find(".summary > p").Each(func(_ int, s *goquery.Selection) {
		if v := text(s); v != "" {
			summary = append(summary, v)
		}
	})
	d.Description = strings.Join(summary, " ")
	if alt := text(doc.Find(".alt_name")); alt != "" {
		d.Description += "\n\nAlt name(s): " + alt
	}
	d.Description = strings.TrimSpace(d.Description)
	status := text(doc.Find(".value.status").First())
	switch {
	case strings.Contains(status, "Ongoing"):
		d.Status = sourcekit.StatusOngoing
	case strings.Contains(status, "Completed"):
		d.Status = sourcekit.StatusCompleted
	}
	doc.Find(".genres > a").Each(func(_ int, a *goquery.Selection) {
		if v := text(a); v != "" {
			d.Genres = append(d.Genres, v)
		}
	})
	return d, nil
}

func (k *mangakatana) cover(doc *goquery.Document) string {
	if src := doc.Find("div.media div.cover img").First().AttrOr("src", ""); src != "" {
		return sourcekit.Abs(k.base, src)
	}
	return ""
}

func (k *mangakatana) Chapters(ctx context.Context, ref sourcekit.Ref) ([]sourcekit.Chapter, error) {
	doc, _, err := k.page(ctx, ref)
	if err != nil {
		return nil, err
	}
	var out []sourcekit.Chapter
	doc.Find("tr:has(.chapter)").Each(func(_ int, row *goquery.Selection) {
		a := row.Find("a").First()
		path := sourcekit.Path(sourcekit.Abs(k.base, a.AttrOr("href", "")))
		if path == "" {
			return
		}
		name := text(a)
		ch := sourcekit.Chapter{URL: path, Name: name, Number: chapterNumber(name), WebURL: k.base + path}
		if t, err := time.Parse("Jan-02-2006", text(row.Find(".update_time").First())); err == nil {
			ch.UploadedAt = &t
		}
		out = append(out, ch)
	})
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no chapters listed", sourcekit.ErrNotFound)
	}
	return out, nil
}

var (
	// the reader script hands one array to the images' data-src
	mangakatanaArrayNameRe = regexp.MustCompile(`data-src['"],\s*(\w+)`)
	mangakatanaImageRe     = regexp.MustCompile(`'([^']*)'`)
)

func (k *mangakatana) Pages(ctx context.Context, ch sourcekit.PageRef) ([]sourcekit.PageImage, error) {
	path := sourcekit.Path(ch.URL)
	if !strings.HasPrefix(path, "/manga/") {
		return nil, fmt.Errorf("%q is not a chapter url", ch.URL)
	}
	req := sourcekit.Request{URL: k.base + path, Headers: k.headers()}
	if k.server != "" {
		req.Query = url.Values{"sv": {k.server}}
	}
	doc, err := k.c.Document(ctx, req)
	if err != nil {
		return nil, err
	}
	urls := mangakatanaImages(doc)
	pages := make([]sourcekit.PageImage, 0, len(urls))
	for _, u := range urls {
		pages = append(pages, sourcekit.PageImage{Index: len(pages), URL: sourcekit.Abs(k.base, u),
			Headers: map[string]string{"Referer": k.base + "/"}})
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: the chapter has no pages", sourcekit.ErrNotFound)
	}
	return pages, nil
}

// mangakatanaImages reads the page images out of the reader script: it names
// the array its images take their data-src from, and that array is declared
// in the same script as "var <name>=['...','...']".
func mangakatanaImages(doc *goquery.Document) []string {
	var script string
	doc.Find("script").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if data := s.Text(); strings.Contains(data, "data-src") {
			script = data
			return false
		}
		return true
	})
	name := mangakatanaArrayNameRe.FindStringSubmatch(script)
	if name == nil {
		return nil
	}
	array := regexp.MustCompile(`var ` + regexp.QuoteMeta(name[1]) + `=\[([^\[]*)]`).FindStringSubmatch(script)
	if array == nil {
		return nil
	}
	var out []string
	for _, m := range mangakatanaImageRe.FindAllStringSubmatch(array[1], -1) {
		if m[1] != "" {
			out = append(out, m[1])
		}
	}
	return out
}

func (k *mangakatana) headers() map[string]string {
	return map[string]string{"Referer": k.base + "/"}
}

// mangakatanaOwnText is an element's own text, leaving out its children's
// (a title link also holds a badge).
func mangakatanaOwnText(s *goquery.Selection) string {
	var b strings.Builder
	s.Contents().Each(func(_ int, c *goquery.Selection) {
		if n := c.Get(0); n != nil && n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
	})
	return strings.Join(strings.Fields(b.String()), " ")
}
