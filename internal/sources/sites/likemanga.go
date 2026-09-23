package sites

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// LikeManga has no API, so this reads its pages: an advanced-search page for
// listings, a manga page with the first chapters (the rest come from an
// ajax endpoint), and a reader page whose image list is packed in a token.
// The id is the one Mihon's "LikeManga" extension has.
var likemangaID = sourcekit.KeiyoushiID("LikeManga", "en", 1)

const likemangaSite = "https://likemanga.ink"

func init() {
	sourcekit.Register(likemangaID, func(d sourcekit.Deps) sourcekit.Site {
		return &likemanga{c: d.Client, base: likemangaSite}
	})
}

type likemanga struct {
	c *sourcekit.Client
	// base is the address to talk to (tests point it at a recorded copy).
	base string
}

func (l *likemanga) Info() sourcekit.Info {
	return sourcekit.Info{ID: likemangaID, Name: "LikeManga", Lang: "en", BaseURL: l.base, SupportsBrowse: true,
		IconURL: l.base + "/favicon.ico"}
}

// Politeness: the extension holds itself to one request every two seconds.
func (l *likemanga) Politeness() sourcekit.Politeness {
	return sourcekit.Politeness{RequestsPerMinute: 30, MaxConcurrent: 1}
}

// ---- search -----------------------------------------------------------------

func (l *likemanga) Search(ctx context.Context, query string, page int) (sourcekit.Results, error) {
	return l.list(ctx, page, strings.TrimSpace(query), "")
}

func (l *likemanga) Popular(ctx context.Context, page int) (sourcekit.Results, error) {
	return l.list(ctx, page, "", "top-manga")
}

// Latest sorts by the latest chapter ("lastest" is the site's spelling).
func (l *likemanga) Latest(ctx context.Context, page int) (sourcekit.Results, error) {
	return l.list(ctx, page, "", "lastest-chap")
}

func (l *likemanga) list(ctx context.Context, page int, keyword, sort string) (sourcekit.Results, error) {
	q := url.Values{"act": {"searchadvance"}}
	if sort != "" {
		q.Set("f[sortby]", sort)
	}
	if keyword != "" {
		q.Set("f[keyword]", keyword)
	}
	if page > 1 {
		q.Set("pageNum", strconv.Itoa(page))
	}
	doc, err := l.c.Document(ctx, sourcekit.Request{URL: l.base + "/", Query: q, Headers: l.headers()})
	if err != nil {
		return sourcekit.Results{}, err
	}
	var res sourcekit.Results
	doc.Find("div.card-body div.card").Each(func(_ int, card *goquery.Selection) {
		path := sourcekit.Path(sourcekit.Abs(l.base, card.Find("a").First().AttrOr("href", "")))
		title := text(card.Find(".title-manga"))
		if path == "" || path == "/" || title == "" || res.Has(path) {
			return
		}
		res.Mangas = append(res.Mangas, sourcekit.Manga{URL: path, Title: title, CoverURL: l.img(card.Find("img").First())})
	})
	res.HasNext = doc.Find(`ul.pagination a:contains("»")`).Length() > 0
	return res, nil
}

// ---- one manga --------------------------------------------------------------

func (l *likemanga) page(ctx context.Context, ref sourcekit.Ref) (*goquery.Document, string, error) {
	path := sourcekit.Path(strings.TrimSpace(ref.URL))
	if path == "" || path == "/" || strings.HasPrefix(path, "/?") {
		return nil, "", fmt.Errorf("%q is not a manga url", ref.URL)
	}
	doc, err := l.c.Document(ctx, sourcekit.Request{URL: l.base + path, Headers: l.headers()})
	if err != nil {
		var se *sourcekit.StatusError
		if errorsAs(err, &se) && se.Code == 404 {
			return nil, "", fmt.Errorf("%w: %s", sourcekit.ErrNotFound, path)
		}
		return nil, "", err
	}
	return doc, path, nil
}

func (l *likemanga) Details(ctx context.Context, ref sourcekit.Ref) (sourcekit.Details, error) {
	doc, path, err := l.page(ctx, ref)
	if err != nil {
		return sourcekit.Details{}, err
	}
	d := sourcekit.Details{Manga: sourcekit.Manga{URL: path, Title: text(doc.Find("#title-detail-manga")),
		CoverURL: l.img(doc.Find(".detail-info img").First())}, WebURL: l.base + path, Status: sourcekit.StatusUnknown}
	if d.Title == "" {
		return sourcekit.Details{}, fmt.Errorf("%w: no manga at %s", sourcekit.ErrNotFound, path)
	}
	d.Description = text(doc.Find("#summary_shortened").First())
	doc.Find(`.list-info a[href*="/genres/"]`).Each(func(_ int, a *goquery.Selection) {
		if g := text(a); g != "" {
			d.Genres = append(d.Genres, g)
		}
	})
	status := strings.ToLower(text(doc.Find(".list-info .status p:nth-child(2)").First()))
	switch {
	case strings.Contains(status, "complete"):
		d.Status = sourcekit.StatusCompleted
	case strings.Contains(status, "in process"):
		d.Status = sourcekit.StatusOngoing
	case strings.Contains(status, "pause"):
		d.Status = sourcekit.StatusHiatus
	}
	if a := text(doc.Find(".list-info .author p:nth-child(2)").First()); a != "Updating" {
		d.Author = a
	}
	return d, nil
}

var likemangaLastPageRe = regexp.MustCompile(`load_list_chapter\((\d+)\)`)

// Chapters reads the manga page's chapters, then the rest of the list page
// by page from the site's ajax endpoint, as its own pager does.
func (l *likemanga) Chapters(ctx context.Context, ref sourcekit.Ref) ([]sourcekit.Chapter, error) {
	doc, _, err := l.page(ctx, ref)
	if err != nil {
		return nil, err
	}
	out := l.chapters(doc.Selection)
	last := 0
	if m := likemangaLastPageRe.FindStringSubmatch(doc.Find("div.chapters_pagination a:not(.next)").Last().AttrOr("onclick", "")); m != nil {
		last, _ = strconv.Atoi(m[1])
	}
	id, idErr := strconv.Atoi(strings.TrimSpace(doc.Find("#title-detail-manga").AttrOr("data-manga", "")))
	for p := 2; p <= last && idErr == nil; p++ {
		q := url.Values{"act": {"ajax"}, "code": {"load_list_chapter"}, "manga_id": {strconv.Itoa(id)},
			"page_num": {strconv.Itoa(p)}, "chap_id": {"0"}, "keyword": {""}}
		var page struct {
			ListChap string `json:"list_chap"`
		}
		// like the extension, a page that fails is skipped rather than
		// failing the whole list
		if err := l.c.JSON(ctx, sourcekit.Request{URL: l.base + "/", Query: q, Headers: l.headers()}, &page); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		frag, err := goquery.NewDocumentFromReader(strings.NewReader(page.ListChap))
		if err != nil {
			continue
		}
		out = append(out, l.chapters(frag.Selection)...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no chapters listed", sourcekit.ErrNotFound)
	}
	return out, nil
}

func (l *likemanga) chapters(s *goquery.Selection) []sourcekit.Chapter {
	var out []sourcekit.Chapter
	s.Find(".wp-manga-chapter").Each(func(_ int, row *goquery.Selection) {
		path := sourcekit.Path(sourcekit.Abs(l.base, row.Find("a").First().AttrOr("href", "")))
		if path == "" {
			return
		}
		name := text(row.Find("a"))
		ch := sourcekit.Chapter{URL: path, Name: name, Number: chapterNumber(name), WebURL: l.base + path}
		if t, err := time.Parse("January 2, 2006", text(row.Find(".chapter-release-date").First())); err == nil {
			ch.UploadedAt = &t
		}
		out = append(out, ch)
	})
	return out
}

func (l *likemanga) Pages(ctx context.Context, ch sourcekit.PageRef) ([]sourcekit.PageImage, error) {
	path := sourcekit.Path(ch.URL)
	if path == "" || path == "/" {
		return nil, fmt.Errorf("%q is not a chapter url", ch.URL)
	}
	doc, err := l.c.Document(ctx, sourcekit.Request{URL: l.base + path, Headers: l.headers()})
	if err != nil {
		return nil, err
	}
	var urls []string
	if token, ok := doc.Find("div.reading input#next_img_token").First().Attr("value"); ok {
		cdn, ok := doc.Find("div.reading #currentlink").First().Attr("value")
		if !ok {
			return nil, fmt.Errorf("the chapter page has no image server")
		}
		images, err := likemangaTokenImages(token)
		if err != nil {
			return nil, err
		}
		for _, img := range images {
			urls = append(urls, cdn+"/"+img)
		}
	} else {
		doc.Find("div.reading-detail.box_doc img").Each(func(_ int, img *goquery.Selection) {
			if img.ParentsFiltered("noscript").Length() > 0 {
				return
			}
			if u := l.img(img); u != "" {
				urls = append(urls, u)
			}
		})
	}
	var pages []sourcekit.PageImage
	for _, u := range urls {
		pages = append(pages, sourcekit.PageImage{Index: len(pages), URL: u, Headers: map[string]string{"Referer": l.base + "/"}})
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: the chapter has no pages", sourcekit.ErrNotFound)
	}
	return pages, nil
}

// likemangaTokenImages unpacks the reader's image token: a JWT-like
// "head.payload.sig" whose payload is base64 JSON {"data": "..."}, where data
// is in turn a base64 JSON list of image paths.
func likemangaTokenImages(token string) ([]string, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("the image token is malformed")
	}
	payload, err := likemangaBase64(parts[1])
	if err != nil {
		return nil, fmt.Errorf("the image token: %w", err)
	}
	var body struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, fmt.Errorf("the image token: %w", err)
	}
	list, err := likemangaBase64(body.Data)
	if err != nil {
		return nil, fmt.Errorf("the image list: %w", err)
	}
	var images []string
	if err := json.Unmarshal(list, &images); err != nil {
		return nil, fmt.Errorf("the image list: %w", err)
	}
	return images, nil
}

// likemangaBase64 decodes base64 whether it is padded or not, and in either
// alphabet.
func likemangaBase64(s string) ([]byte, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	s = strings.NewReplacer("-", "+", "_", "/", "\n", "", "\r", "").Replace(s)
	return base64.RawStdEncoding.DecodeString(s)
}

// img is an image's address, from whichever attribute the site's lazy
// loading left it in.
func (l *likemanga) img(s *goquery.Selection) string {
	if s.Length() == 0 {
		return ""
	}
	for _, attr := range []string{"data-cfsrc", "data-src", "data-lazy-src"} {
		if v, ok := s.Attr(attr); ok {
			return sourcekit.Abs(l.base, v)
		}
	}
	if v, ok := s.Attr("srcset"); ok {
		if f := strings.Fields(v); len(f) > 0 {
			return sourcekit.Abs(l.base, f[0])
		}
		return ""
	}
	return sourcekit.Abs(l.base, s.AttrOr("src", ""))
}

func (l *likemanga) headers() map[string]string {
	return map[string]string{"Referer": l.base + "/"}
}
