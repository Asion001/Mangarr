package sites

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// WeebCentral has no API, so this reads its pages. The id is the one Mihon's
// "Weeb Central" extension has, so a library imported from a Mihon backup
// links here instead of to an unknown catalog.
var weebcentralID = sourcekit.KeiyoushiID("Weeb Central", "en", 1)

const weebcentralSite = "https://weebcentral.com"

func init() {
	sourcekit.Register(weebcentralID, func(d sourcekit.Deps) sourcekit.Site {
		return &weebcentral{c: d.Client, base: weebcentralSite}
	})
}

type weebcentral struct {
	c    *sourcekit.Client
	base string
}

func (w *weebcentral) Info() sourcekit.Info {
	return sourcekit.Info{ID: weebcentralID, Name: "WeebCentral", Lang: "en", BaseURL: w.base, SupportsBrowse: true,
		IconURL: w.base + "/favicon.ico"}
}

// Politeness: a scraped site, so stay gentle.
func (w *weebcentral) Politeness() sourcekit.Politeness {
	return sourcekit.Politeness{RequestsPerMinute: 60, MaxConcurrent: 2}
}

const weebcentralPageSize = 32

func (w *weebcentral) Search(ctx context.Context, query string, page int) (sourcekit.Results, error) {
	if page < 1 {
		page = 1
	}
	q := url.Values{}
	q.Set("text", strings.TrimSpace(query))
	q.Set("sort", "Best Match")
	q.Set("order", "Descending")
	q.Set("limit", strconv.Itoa(weebcentralPageSize))
	q.Set("offset", strconv.Itoa((page-1)*weebcentralPageSize))
	q.Set("display_mode", "Full Display")
	return w.list(ctx, w.base+"/search/data", q)
}

func (w *weebcentral) Popular(ctx context.Context, page int) (sourcekit.Results, error) {
	return w.browse(ctx, page, "Popularity")
}

func (w *weebcentral) Latest(ctx context.Context, page int) (sourcekit.Results, error) {
	return w.browse(ctx, page, "Latest Updates")
}

func (w *weebcentral) browse(ctx context.Context, page int, sort string) (sourcekit.Results, error) {
	if page < 1 {
		page = 1
	}
	q := url.Values{}
	q.Set("sort", sort)
	q.Set("order", "Descending")
	q.Set("limit", strconv.Itoa(weebcentralPageSize))
	q.Set("offset", strconv.Itoa((page-1)*weebcentralPageSize))
	q.Set("display_mode", "Full Display")
	return w.list(ctx, w.base+"/search/data", q)
}

func (w *weebcentral) list(ctx context.Context, endpoint string, q url.Values) (sourcekit.Results, error) {
	doc, err := w.c.Document(ctx, sourcekit.Request{URL: endpoint, Query: q, Headers: w.headers("")})
	if err != nil {
		return sourcekit.Results{}, err
	}
	// a result is two links to the same series: one wrapping the cover, one
	// carrying the title, so they're merged by the series they point at
	var res sourcekit.Results
	order := []string{}
	found := map[string]*sourcekit.Manga{}
	doc.Find(`a[href*="/series/"]`).Each(func(_ int, a *goquery.Selection) {
		path := seriesPath(a.AttrOr("href", ""))
		if path == "" {
			return
		}
		m, ok := found[path]
		if !ok {
			m = &sourcekit.Manga{URL: path}
			found[path] = m
			order = append(order, path)
		}
		if src, ok := a.Find("img[src]").First().Attr("src"); ok {
			if m.CoverURL == "" {
				m.CoverURL = sourcekit.Abs(w.base, src)
			}
			return // the cover's link also carries badges: not the title
		}
		if title := text(a); title != "" && m.Title == "" {
			m.Title = title
		}
	})
	for _, path := range order {
		if m := found[path]; m.Title != "" {
			res.Mangas = append(res.Mangas, *m)
		}
	}
	res.HasNext = len(res.Mangas) >= weebcentralPageSize
	return res, nil
}

func (w *weebcentral) Details(ctx context.Context, ref sourcekit.Ref) (sourcekit.Details, error) {
	id := seriesID(ref.URL)
	if id == "" {
		return sourcekit.Details{}, fmt.Errorf("%q is not a series url", ref.URL)
	}
	doc, err := w.c.Document(ctx, sourcekit.Request{URL: w.base + "/series/" + id, Headers: w.headers("")})
	if err != nil {
		return sourcekit.Details{}, err
	}
	d := sourcekit.Details{Manga: sourcekit.Manga{URL: "/series/" + id}, Status: sourcekit.StatusUnknown,
		WebURL: w.base + "/series/" + id}
	d.Title = text(doc.Find("h1").First())
	d.CoverURL, _ = doc.Find(`img[alt$="cover"]`).First().Attr("src")
	d.Description = strings.TrimSpace(doc.Find("li:has(strong:contains('Description')) p").First().Text())
	d.Author = strings.Join(fieldLinks(doc, "Author(s)"), ", ")
	d.Genres = fieldLinks(doc, "Tags(s)")
	if len(d.Genres) == 0 {
		d.Genres = fieldLinks(doc, "Tag(s)")
	}
	if s := fieldLinks(doc, "Status"); len(s) > 0 {
		d.Status = weebcentralStatus(s[0])
	}
	return d, nil
}

// text is an element's text with its whitespace collapsed.
func text(s *goquery.Selection) string {
	return strings.Join(strings.Fields(s.Text()), " ")
}

// fieldLinks reads the values of one "<strong>Label</strong> a, a, a" row.
func fieldLinks(doc *goquery.Document, label string) []string {
	var out []string
	doc.Find("li").EachWithBreak(func(_ int, li *goquery.Selection) bool {
		if strings.TrimSpace(li.Find("strong").First().Text()) != label+":" &&
			strings.TrimSpace(li.Find("strong").First().Text()) != label {
			return true
		}
		li.Find("a").Each(func(_ int, a *goquery.Selection) {
			if v := strings.TrimSpace(a.Text()); v != "" {
				out = append(out, v)
			}
		})
		return false
	})
	return out
}

func weebcentralStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ongoing":
		return sourcekit.StatusOngoing
	case "complete", "completed":
		return sourcekit.StatusCompleted
	case "hiatus":
		return sourcekit.StatusHiatus
	case "canceled", "cancelled":
		return sourcekit.StatusCancelled
	}
	return sourcekit.StatusUnknown
}

func (w *weebcentral) Chapters(ctx context.Context, ref sourcekit.Ref) ([]sourcekit.Chapter, error) {
	id := seriesID(ref.URL)
	if id == "" {
		return nil, fmt.Errorf("%q is not a series url", ref.URL)
	}
	doc, err := w.c.Document(ctx, sourcekit.Request{URL: w.base + "/series/" + id + "/full-chapter-list",
		Headers: w.headers(w.base + "/series/" + id)})
	if err != nil {
		return nil, err
	}
	var out []sourcekit.Chapter
	doc.Find(`a[href*="/chapters/"]`).Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		path := sourcekit.Path(href)
		if !strings.HasPrefix(path, "/chapters/") {
			return
		}
		name := text(a.Find("span.grow span").First())
		if name == "" {
			name = text(a)
		}
		ch := sourcekit.Chapter{URL: path, Name: name, Number: chapterNumber(name), WebURL: w.base + path}
		if at, ok := a.Find("time[datetime]").First().Attr("datetime"); ok {
			if t, err := time.Parse(time.RFC3339, at); err == nil {
				ch.UploadedAt = &t
			}
		}
		out = append(out, ch)
	})
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no chapters listed", sourcekit.ErrNotFound)
	}
	return out, nil
}

// chapterNumber reads the number out of "Chapter 386" (-1 when there's none).
func chapterNumber(name string) float64 {
	fields := strings.Fields(strings.ToLower(name))
	for i, f := range fields {
		if (f == "chapter" || f == "ch." || f == "ch") && i+1 < len(fields) {
			if n, err := strconv.ParseFloat(strings.Trim(fields[i+1], ":-"), 64); err == nil {
				return n
			}
		}
	}
	for _, f := range fields {
		if n, err := strconv.ParseFloat(f, 64); err == nil {
			return n
		}
	}
	return -1
}

func (w *weebcentral) Pages(ctx context.Context, ch sourcekit.PageRef) ([]sourcekit.PageImage, error) {
	path := sourcekit.Path(ch.URL)
	if !strings.HasPrefix(path, "/chapters/") {
		return nil, fmt.Errorf("%q is not a chapter url", ch.URL)
	}
	q := url.Values{"is_prev": {"False"}, "current_page": {"1"}, "reading_style": {"long_strip"}}
	doc, err := w.c.Document(ctx, sourcekit.Request{URL: w.base + path + "/images", Query: q, Headers: w.headers(w.base + path)})
	if err != nil {
		return nil, err
	}
	var pages []sourcekit.PageImage
	doc.Find("section img[src]").Each(func(i int, s *goquery.Selection) {
		src, _ := s.Attr("src")
		if src == "" {
			return
		}
		pages = append(pages, sourcekit.PageImage{Index: len(pages), URL: sourcekit.Abs(w.base, src),
			Headers: map[string]string{"Referer": w.base + "/"}})
	})
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: the chapter has no pages", sourcekit.ErrNotFound)
	}
	return pages, nil
}

// headers are what the site expects of a browser.
func (w *weebcentral) headers(referer string) map[string]string {
	if referer == "" {
		referer = w.base + "/"
	}
	return map[string]string{"Referer": referer}
}

// seriesPath is the "/series/<id>" identity of a link (the slug after it
// changes with the title, so it is dropped).
func seriesPath(href string) string {
	if id := seriesID(href); id != "" {
		return "/series/" + id
	}
	return ""
}

func seriesID(raw string) string {
	p := strings.Trim(sourcekit.Path(raw), "/")
	parts := strings.Split(p, "/")
	for i, part := range parts {
		if part == "series" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
