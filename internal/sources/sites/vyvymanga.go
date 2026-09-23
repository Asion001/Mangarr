package sites

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// VyvyManga has no API, so this reads its pages. The id is the one Mihon's
// "VyvyManga" extension has.
//
// Its chapter links change over time, so a chapter's identity is not its
// link but, as in the extension, the last ten hex digits of
// md5("<date in ms>:<title>"); the link itself is kept as the chapter's id
// and looked up again from the manga's page when it has gone stale.
var vyvymangaID = sourcekit.KeiyoushiID("VyvyManga", "en", 1)

const vyvymangaSite = "https://mangavyvy.net"

func init() {
	sourcekit.Register(vyvymangaID, func(d sourcekit.Deps) sourcekit.Site {
		return &vyvymanga{c: d.Client, base: vyvymangaSite, now: time.Now}
	})
}

type vyvymanga struct {
	c *sourcekit.Client
	// base is the address to talk to (tests point it at a recorded copy).
	base string
	// now is the clock "2 hours ago" is read against.
	now func() time.Time
}

func (v *vyvymanga) Info() sourcekit.Info {
	return sourcekit.Info{ID: vyvymangaID, Name: "VyvyManga", Lang: "en", BaseURL: v.base, SupportsBrowse: true,
		IconURL: v.base + "/favicon.ico"}
}

// Politeness: a scraped site with no limit in the extension, so stay gentle.
func (v *vyvymanga) Politeness() sourcekit.Politeness {
	return sourcekit.Politeness{RequestsPerMinute: 60, MaxConcurrent: 2}
}

// ---- search -----------------------------------------------------------------

// Search sends the site's search form as the extension does with its
// filters left alone: title contains the text, any status, most viewed.
func (v *vyvymanga) Search(ctx context.Context, query string, page int) (sourcekit.Results, error) {
	if page < 1 {
		page = 1
	}
	q := url.Values{}
	q.Set("q", strings.TrimSpace(query))
	q.Set("page", strconv.Itoa(page))
	q.Set("search_po", "0")
	q.Set("author_po", "0")
	q.Set("author", "")
	q.Set("completed", "2")
	q.Set("sort", "viewed")
	q.Set("sort_type", "desc")
	return v.list(ctx, q)
}

func (v *vyvymanga) Popular(ctx context.Context, page int) (sourcekit.Results, error) {
	q := url.Values{}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	return v.list(ctx, q)
}

func (v *vyvymanga) Latest(ctx context.Context, page int) (sourcekit.Results, error) {
	q := url.Values{"sort": {"updated_at"}}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	return v.list(ctx, q)
}

func (v *vyvymanga) list(ctx context.Context, q url.Values) (sourcekit.Results, error) {
	doc, err := v.c.Document(ctx, sourcekit.Request{URL: v.base + "/search", Query: q, Headers: v.headers()})
	if err != nil {
		return sourcekit.Results{}, err
	}
	var res sourcekit.Results
	doc.Find(".comic-item").Each(func(_ int, item *goquery.Selection) {
		path := sourcekit.Path(sourcekit.Abs(v.base, item.Find("a").First().AttrOr("href", "")))
		title := text(item.Find(".comic-title").First())
		if path == "" || title == "" || res.Has(path) {
			return
		}
		m := sourcekit.Manga{URL: path, Title: title}
		if src := item.Find(".comic-image img.image.lozad").First().AttrOr("data-src", ""); src != "" {
			m.CoverURL = sourcekit.Abs(v.base, src)
		}
		res.Mangas = append(res.Mangas, m)
	})
	res.HasNext = doc.Find("[rel=next]").Length() > 0
	return res, nil
}

// ---- one manga --------------------------------------------------------------

func (v *vyvymanga) page(ctx context.Context, ref sourcekit.Ref) (*goquery.Document, string, error) {
	path := vyvymangaPath(ref.URL)
	if path == "" {
		return nil, "", fmt.Errorf("%q is not a manga url", ref.URL)
	}
	doc, err := v.c.Document(ctx, sourcekit.Request{URL: v.base + path, Headers: v.headers()})
	if err != nil {
		var se *sourcekit.StatusError
		if errorsAs(err, &se) && se.Code == 404 {
			return nil, "", fmt.Errorf("%w: %s", sourcekit.ErrNotFound, path)
		}
		return nil, "", err
	}
	return doc, path, nil
}

func (v *vyvymanga) Details(ctx context.Context, ref sourcekit.Ref) (sourcekit.Details, error) {
	doc, path, err := v.page(ctx, ref)
	if err != nil {
		return sourcekit.Details{}, err
	}
	d := sourcekit.Details{Manga: sourcekit.Manga{URL: path, Title: text(doc.Find("h1").First())},
		WebURL: v.base + path, Status: sourcekit.StatusUnknown}
	if d.Title == "" {
		return sourcekit.Details{}, fmt.Errorf("%w: no manga at %s", sourcekit.ErrNotFound, path)
	}
	d.Artist = text(doc.Find(`.pre-title:contains("Artist") ~ a`).First())
	d.Author = text(doc.Find(`.pre-title:contains("Author") ~ a`).First())
	d.Description = text(doc.Find(".summary > .content").First())
	doc.Find(`.pre-title:contains("Genres") ~ a`).Each(func(_ int, a *goquery.Selection) {
		if g := text(a); g != "" {
			d.Genres = append(d.Genres, g)
		}
	})
	switch text(doc.Find(`.pre-title:contains("Status") ~ span:not(.space)`).First()) {
	case "Ongoing":
		d.Status = sourcekit.StatusOngoing
	case "Completed":
		d.Status = sourcekit.StatusCompleted
	}
	if src := doc.Find(".img-manga img").First().AttrOr("src", ""); src != "" {
		d.CoverURL = sourcekit.Abs(v.base, src)
	}
	return d, nil
}

func (v *vyvymanga) Chapters(ctx context.Context, ref sourcekit.Ref) ([]sourcekit.Chapter, error) {
	doc, _, err := v.page(ctx, ref)
	if err != nil {
		return nil, err
	}
	now := v.now()
	var out []sourcekit.Chapter
	doc.Find(".list-group > a").Each(func(_ int, a *goquery.Selection) {
		title := text(a.Find("span").First())
		href := a.AttrOr("href", "")
		if title == "" || href == "" {
			return
		}
		link := sourcekit.Abs(v.base, href)
		at, key := vyvymangaDate(text(a.ChildrenFiltered("p").First()), now)
		ch := sourcekit.Chapter{URL: vyvymangaKey(key, title), ID: link, Name: title, Number: chapterNumber(title), WebURL: link}
		if !at.IsZero() {
			ch.UploadedAt = &at
		}
		out = append(out, ch)
	})
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no chapters listed", sourcekit.ErrNotFound)
	}
	return out, nil
}

// vyvymangaKey is a chapter's identity, as the extension makes it.
func vyvymangaKey(ms int64, title string) string {
	sum := md5.Sum([]byte(strconv.FormatInt(ms, 10) + ":" + title))
	h := hex.EncodeToString(sum[:])
	return h[len(h)-10:]
}

var vyvymangaNumberRe = regexp.MustCompile(`\d+`)

// vyvymangaDate reads a chapter's date, "Jan 05, 2024" or "3 hours ago",
// and the milliseconds its identity is made from. For a written-out date
// that is the day's start in UTC, as in the extension. A relative date there
// uses the moment it was read, which would change the identity at every
// refresh, so here it is the start of that day too: the key then stays put
// when the site starts writing the date out.
func vyvymangaDate(s string, now time.Time) (time.Time, int64) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, 0
	}
	if !strings.HasSuffix(s, "ago") {
		t, err := time.Parse("Jan 02, 2006", s)
		if err != nil {
			return time.Time{}, 0
		}
		return t, t.UnixMilli()
	}
	n, err := strconv.Atoi(vyvymangaNumberRe.FindString(s))
	if err != nil {
		return time.Time{}, 0
	}
	var t time.Time
	switch {
	case strings.Contains(s, "day"):
		t = now.Add(-time.Duration(n) * 24 * time.Hour)
	case strings.Contains(s, "hour"):
		t = now.Add(-time.Duration(n) * time.Hour)
	case strings.Contains(s, "minute"):
		t = now.Add(-time.Duration(n) * time.Minute)
	case strings.Contains(s, "second"):
		t = now.Add(-time.Duration(n) * time.Second)
	default:
		return time.Time{}, 0
	}
	day := t.UTC().Truncate(24 * time.Hour)
	return t, day.UnixMilli()
}

func (v *vyvymanga) Pages(ctx context.Context, ch sourcekit.PageRef) ([]sourcekit.PageImage, error) {
	link := ch.ID
	if !strings.HasPrefix(link, "http") && strings.HasPrefix(ch.URL, "http") {
		link = ch.URL
	}
	var pages []sourcekit.PageImage
	var err error
	if link != "" {
		pages, err = v.pages(ctx, link)
		if err == nil && len(pages) > 0 {
			return pages, nil
		}
	}
	// the link is missing or stale: find the chapter again on the manga's page
	if ch.Manga.URL == "" {
		if err == nil {
			err = fmt.Errorf("%w: the chapter has no pages", sourcekit.ErrNotFound)
		}
		return nil, err
	}
	chapters, cerr := v.Chapters(ctx, ch.Manga)
	if cerr != nil {
		return nil, errors.Join(err, cerr)
	}
	for _, c := range chapters {
		if c.URL == ch.URL && c.ID != link {
			if pages, err = v.pages(ctx, c.ID); err == nil && len(pages) > 0 {
				return pages, nil
			}
			break
		}
	}
	if err == nil {
		err = fmt.Errorf("%w: chapter %s", sourcekit.ErrNotFound, ch.URL)
	}
	return nil, err
}

func (v *vyvymanga) pages(ctx context.Context, link string) ([]sourcekit.PageImage, error) {
	doc, err := v.c.Document(ctx, sourcekit.Request{URL: link, Headers: v.headers()})
	if err != nil {
		return nil, err
	}
	var pages []sourcekit.PageImage
	doc.Find("img.d-block").Each(func(_ int, img *goquery.Selection) {
		if src := img.AttrOr("data-src", ""); src != "" {
			pages = append(pages, sourcekit.PageImage{Index: len(pages), URL: sourcekit.Abs(link, src),
				Headers: map[string]string{"Referer": v.base + "/"}})
		}
	})
	return pages, nil
}

func (v *vyvymanga) headers() map[string]string {
	return map[string]string{"Referer": v.base + "/"}
}

// vyvymangaPath is "/manga/<slug>" out of a manga link.
func vyvymangaPath(raw string) string {
	parts := strings.Split(strings.Trim(sourcekit.Path(raw), "/"), "/")
	if len(parts) >= 2 && parts[0] == "manga" && parts[1] != "" {
		return "/manga/" + parts[1]
	}
	return ""
}
