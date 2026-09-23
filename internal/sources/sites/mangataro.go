package sites

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// MangaTaro is a WordPress site with a few JSON endpoints of its own: a
// search box, a browse listing, the WordPress REST API for a manga, and
// "auth" endpoints for chapters and pages. The id is the one Mihon's
// "MangaTaro" extension has.
//
// A manga's url is kept the way the extension writes it, a small JSON object
// ({"id":"123","slug":"title"}), so a manga imported from a Mihon backup is
// the same manga here.
var mangataroID = sourcekit.KeiyoushiID("MangaTaro", "en", 1)

const (
	mangataroSite = "https://mangataro.org"
	// mangataroBrowseSize is how many titles the browse listing sends a page.
	mangataroBrowseSize = 24
)

func init() {
	sourcekit.Register(mangataroID, func(d sourcekit.Deps) sourcekit.Site {
		return &mangataro{c: d.Client, base: mangataroSite, now: time.Now}
	})
}

type mangataro struct {
	c *sourcekit.Client
	// base is the address to talk to (tests point it at a recorded copy).
	base string
	// now is the clock the chapter list's token is made from.
	now func() time.Time
}

func (m *mangataro) Info() sourcekit.Info {
	return sourcekit.Info{ID: mangataroID, Name: "MangaTaro", Lang: "en", BaseURL: m.base, SupportsBrowse: true,
		IconURL: m.base + "/favicon.ico"}
}

// Politeness: the extension sets no limit, so stay gentle.
func (m *mangataro) Politeness() sourcekit.Politeness {
	return sourcekit.Politeness{RequestsPerMinute: 60, MaxConcurrent: 2}
}

// ---- search -----------------------------------------------------------------

// mangataroNum is an id the site sends as a number in one place and a
// string in another.
type mangataroNum string

func (n *mangataroNum) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" {
		s = ""
	}
	*n = mangataroNum(s)
	return nil
}

// mangataroURL is a manga's identity as the extension stores it.
type mangataroURL struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Group *int64 `json:"group,omitempty"`
}

func (u mangataroURL) String() string {
	b, _ := json.Marshal(u)
	return string(b)
}

// Search asks the site's search box (what the extension does unless filters
// are applied): one page of up to 25 best matches.
func (m *mangataro) Search(ctx context.Context, query string, page int) (sourcekit.Results, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return m.Popular(ctx, page)
	}
	if page > 1 {
		return sourcekit.Results{}, nil
	}
	body, _ := json.Marshal(struct {
		Limit int    `json:"limit"`
		Query string `json:"query"`
	}{25, q})
	var out struct {
		Results []struct {
			ID        mangataroNum `json:"id"`
			Slug      string       `json:"slug"`
			Title     string       `json:"title"`
			Thumbnail string       `json:"thumbnail"`
			Type      string       `json:"type"`
		} `json:"results"`
	}
	req := sourcekit.Request{Method: "POST", URL: m.base + "/auth/search", Body: body, Headers: m.headers(true)}
	if err := m.c.JSON(ctx, req, &out); err != nil {
		return sourcekit.Results{}, err
	}
	var res sourcekit.Results
	for _, r := range out.Results {
		if r.Type == "Novel" || r.ID == "" || r.Slug == "" {
			continue
		}
		res.Mangas = append(res.Mangas, sourcekit.Manga{URL: mangataroURL{ID: string(r.ID), Slug: r.Slug}.String(),
			ID: string(r.ID), Title: mangataroUnescape(r.Title), CoverURL: r.Thumbnail})
	}
	return res, nil
}

func (m *mangataro) Popular(ctx context.Context, page int) (sourcekit.Results, error) {
	return m.browse(ctx, page, "popular_desc")
}

func (m *mangataro) Latest(ctx context.Context, page int) (sourcekit.Results, error) {
	return m.browse(ctx, page, "post_desc")
}

// browse reads the listing the site's browse page loads. The list filters
// are sent as JSON strings holding a list ("[]"), as the site expects.
func (m *mangataro) browse(ctx context.Context, page int, sort string) (sourcekit.Results, error) {
	if page < 1 {
		page = 1
	}
	body, _ := json.Marshal(struct {
		Page          int    `json:"page"`
		Search        string `json:"search"`
		Years         string `json:"years"`
		Genres        string `json:"genres"`
		Types         string `json:"types"`
		Statuses      string `json:"statuses"`
		Sort          string `json:"sort"`
		GenreMatchMod string `json:"genreMatchMode"`
	}{page, "", "[]", "[]", "[]", "[]", sort, "any"})
	var out []struct {
		ID    mangataroNum `json:"id"`
		URL   string       `json:"url"`
		Title string       `json:"title"`
		Cover string       `json:"cover"`
		Type  string       `json:"type"`
	}
	req := sourcekit.Request{Method: "POST", URL: m.base + "/wp-json/manga/v1/load", Body: body, Headers: m.headers(true)}
	if err := m.c.JSON(ctx, req, &out); err != nil {
		return sourcekit.Results{}, err
	}
	var res sourcekit.Results
	for _, r := range out {
		slug := mangataroSlug(r.URL)
		if r.Type == "Novel" || slug == "" || r.ID == "" {
			continue
		}
		res.Mangas = append(res.Mangas, sourcekit.Manga{URL: mangataroURL{ID: string(r.ID), Slug: slug}.String(),
			ID: string(r.ID), Title: mangataroUnescape(r.Title), CoverURL: r.Cover})
	}
	res.HasNext = len(out) == mangataroBrowseSize
	return res, nil
}

// ---- one manga --------------------------------------------------------------

// mangataroTarget is what the site needs to find a manga: the post id for
// the API, the slug for its page.
type mangataroTarget struct {
	mangataroURL
	// status is read off the manga's page; the API doesn't carry it.
	status string
}

// resolve works out a manga's id and slug from what mangarr stored: the
// extension's JSON url, or a "/manga/<slug>" link, whose page carries the
// id. The page also carries the status, so for details it is read whenever
// the slug is known; when only the id is, the status stays unknown.
func (m *mangataro) resolve(ctx context.Context, ref sourcekit.Ref, withStatus bool) (mangataroTarget, error) {
	t := mangataroTarget{status: sourcekit.StatusUnknown}
	raw := strings.TrimSpace(ref.URL)
	if strings.HasPrefix(raw, "{") {
		if err := json.Unmarshal([]byte(raw), &t.mangataroURL); err != nil {
			return t, fmt.Errorf("%q is not a manga url: %w", ref.URL, err)
		}
	} else {
		t.Slug = mangataroSlug(raw)
		t.ID = ref.ID
	}
	if t.Slug == "" || (t.ID != "" && !withStatus) {
		if t.ID == "" {
			return t, fmt.Errorf("%q is not a manga url", ref.URL)
		}
		return t, nil
	}
	doc, err := m.c.Document(ctx, sourcekit.Request{URL: m.base + "/manga/" + t.Slug, Headers: m.headers(false)})
	if err != nil {
		if t.ID != "" {
			return t, nil // the API still answers without the page
		}
		return t, err
	}
	if id, ok := doc.Find("body").Attr("data-manga-id"); ok && strings.TrimSpace(id) != "" {
		t.ID = strings.TrimSpace(id)
	}
	if t.ID == "" {
		return t, fmt.Errorf("%w: no manga id on %s", sourcekit.ErrNotFound, "/manga/"+t.Slug)
	}
	novel := false
	doc.Find(".capitalize").Each(func(_ int, s *goquery.Selection) {
		v := strings.ToLower(text(s))
		switch {
		case v == "ongoing" && t.status == sourcekit.StatusUnknown:
			t.status = sourcekit.StatusOngoing
		case v == "completed" && t.status == sourcekit.StatusUnknown:
			t.status = sourcekit.StatusCompleted
		}
		if strings.Contains(text(s), "Novel") {
			novel = true
		}
	})
	if novel {
		return t, fmt.Errorf("%w: novels are not supported", sourcekit.ErrUnsupported)
	}
	return t, nil
}

// mangataroTerm is one WordPress taxonomy term (a tag, an author).
type mangataroTerm struct {
	Name     string `json:"name"`
	Taxonomy string `json:"taxonomy"`
}

// mangataroRendered is a WordPress field sent as HTML.
type mangataroRendered struct {
	Rendered string `json:"rendered"`
}

// mangataroPost is a manga as the WordPress REST API sends it with _embed.
type mangataroPost struct {
	ID      mangataroNum      `json:"id"`
	Slug    string            `json:"slug"`
	Title   mangataroRendered `json:"title"`
	Content mangataroRendered `json:"content"`
	Type    string            `json:"type"`
	Embed   struct {
		Media []struct {
			SourceURL string `json:"source_url"`
		} `json:"wp:featuredmedia"`
		Terms [][]mangataroTerm `json:"wp:term"`
	} `json:"_embedded"`
}

// terms lists the names of one taxonomy's terms.
func (p *mangataroPost) terms(taxonomy string) []string {
	var out []string
	for _, group := range p.Embed.Terms {
		if len(group) == 0 || group[0].Taxonomy != taxonomy {
			continue
		}
		for _, t := range group {
			out = append(out, t.Name)
		}
		break
	}
	return out
}

func (m *mangataro) Details(ctx context.Context, ref sourcekit.Ref) (sourcekit.Details, error) {
	t, err := m.resolve(ctx, ref, true)
	if err != nil {
		return sourcekit.Details{}, err
	}
	var p mangataroPost
	// "?_embed" with no value, as WordPress documents it
	req := sourcekit.Request{URL: m.base + "/wp-json/wp/v2/manga/" + url.PathEscape(t.ID) + "?_embed", Headers: m.headers(false)}
	if err := m.c.JSON(ctx, req, &p); err != nil {
		var se *sourcekit.StatusError
		if errorsAs(err, &se) && se.Code == 404 {
			return sourcekit.Details{}, fmt.Errorf("%w: manga %s", sourcekit.ErrNotFound, t.ID)
		}
		return sourcekit.Details{}, err
	}
	if p.ID == "" {
		return sourcekit.Details{}, fmt.Errorf("%w: manga %s", sourcekit.ErrNotFound, t.ID)
	}
	u := mangataroURL{ID: string(p.ID), Slug: p.Slug, Group: t.Group}
	if u.Slug == "" {
		u.Slug = t.Slug
	}
	d := sourcekit.Details{
		Manga:  sourcekit.Manga{URL: u.String(), ID: u.ID, Title: mangataroUnescape(p.Title.Rendered)},
		Status: t.status, WebURL: m.base + "/manga/" + u.Slug,
	}
	if len(p.Embed.Media) > 0 {
		d.CoverURL = p.Embed.Media[0].SourceURL
	}
	if doc, err := goquery.NewDocumentFromReader(strings.NewReader(p.Content.Rendered)); err == nil {
		d.Description = strings.TrimSpace(mangataroUnescape(doc.Text()))
	}
	d.Genres = p.terms("post_tag")
	// the type (Manga, Manhwa, ...) counts as a genre when the tags lack it
	hasType := false
	for _, g := range d.Genres {
		if g == "Manga" || g == "Manhwa" || g == "Manhua" {
			hasType = true
		}
	}
	if !hasType && p.Type != "" {
		d.Genres = append(d.Genres, p.Type)
	}
	d.Author = strings.Join(p.terms("manga_author"), ", ")
	return d, nil
}

// mangataroPlaceholders are what the site writes for "no title" or "no group".
var mangataroPlaceholders = map[string]bool{"": true, "N/A": true, "—": true}

func (m *mangataro) Chapters(ctx context.Context, ref sourcekit.Ref) ([]sourcekit.Chapter, error) {
	t, err := m.resolve(ctx, ref, false)
	if err != nil {
		return nil, err
	}
	now := m.now()
	ts := strconv.FormatInt(now.Unix(), 10)
	q := url.Values{}
	q.Set("manga_id", t.ID)
	q.Set("offset", "0")
	q.Set("limit", "9999")
	q.Set("order", "DESC")
	q.Set("_t", mangataroToken(now))
	q.Set("_ts", ts)
	if t.Group != nil {
		q.Set("group_id", strconv.FormatInt(*t.Group, 10))
	}
	var out struct {
		Chapters []struct {
			URL       string  `json:"url"`
			Chapter   string  `json:"chapter"`
			Title     *string `json:"title"`
			Date      string  `json:"date"`
			GroupName *string `json:"group_name"`
			Language  string  `json:"language"`
		} `json:"chapters"`
	}
	if err := m.c.JSON(ctx, sourcekit.Request{URL: m.base + "/auth/manga-chapters", Query: q, Headers: m.headers(false)}, &out); err != nil {
		return nil, err
	}
	var chapters []sourcekit.Chapter
	for _, c := range out.Chapters {
		if !strings.EqualFold(c.Language, "en") || c.URL == "" {
			continue
		}
		path := sourcekit.Path(strings.TrimSuffix(c.URL, "/"))
		name := "Chapter " + c.Chapter
		if c.Title != nil && !mangataroPlaceholders[*c.Title] {
			name += ": " + mangataroUnescape(*c.Title)
		}
		ch := sourcekit.Chapter{URL: path, Name: name, Number: -1, WebURL: m.base + path}
		if n, err := strconv.ParseFloat(strings.TrimSpace(c.Chapter), 64); err == nil {
			ch.Number = n
		}
		if c.GroupName != nil && !mangataroPlaceholders[*c.GroupName] {
			ch.Scanlator = *c.GroupName
		}
		ch.UploadedAt = mangataroAgo(c.Date, now)
		chapters = append(chapters, ch)
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("%w: no chapters listed", sourcekit.ErrNotFound)
	}
	return chapters, nil
}

// mangataroToken is the "_t" the chapter list wants: the first 16 hex digits
// of md5("<unix seconds>mng_ch_<yyyyMMddHH in UTC>").
func mangataroToken(now time.Time) string {
	sum := md5.Sum([]byte(strconv.FormatInt(now.Unix(), 10) + "mng_ch_" + now.UTC().Format("2006010215")))
	return hex.EncodeToString(sum[:])[:16]
}

var mangataroAgoRe = regexp.MustCompile(`^(\d+)\s+(second|minute|hour|day|week|month|year)s?\s+ago$`)

// mangataroAgo reads "3 days ago", the only way the site dates a chapter.
func mangataroAgo(s string, now time.Time) *time.Time {
	mt := mangataroAgoRe.FindStringSubmatch(strings.TrimSpace(s))
	if mt == nil {
		return nil
	}
	n, err := strconv.Atoi(mt[1])
	if err != nil {
		return nil
	}
	var t time.Time
	switch mt[2] {
	case "second":
		t = now.Add(-time.Duration(n) * time.Second)
	case "minute":
		t = now.Add(-time.Duration(n) * time.Minute)
	case "hour":
		t = now.Add(-time.Duration(n) * time.Hour)
	case "day":
		t = now.AddDate(0, 0, -n)
	case "week":
		t = now.AddDate(0, 0, -7*n)
	case "month":
		t = now.AddDate(0, -n, 0)
	case "year":
		t = now.AddDate(-n, 0, 0)
	}
	return &t
}

func (m *mangataro) Pages(ctx context.Context, ch sourcekit.PageRef) ([]sourcekit.PageImage, error) {
	// a chapter is "/read/<manga>/<name>-<id>": the id is after the last dash
	path := strings.TrimSuffix(sourcekit.Path(ch.URL), "/")
	last := path[strings.LastIndex(path, "/")+1:]
	id := last[strings.LastIndex(last, "-")+1:]
	if id == "" {
		return nil, fmt.Errorf("%q is not a chapter url", ch.URL)
	}
	var out struct {
		Images []string `json:"images"`
	}
	req := sourcekit.Request{URL: m.base + "/auth/chapter-content", Query: url.Values{"chapter_id": {id}}, Headers: m.headers(false)}
	if err := m.c.JSON(ctx, req, &out); err != nil {
		return nil, err
	}
	var pages []sourcekit.PageImage
	for _, img := range out.Images {
		if img = strings.TrimSpace(img); img == "" {
			continue
		}
		pages = append(pages, sourcekit.PageImage{Index: len(pages), URL: sourcekit.Abs(m.base, img),
			Headers: map[string]string{"Referer": m.base + "/"}})
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: the chapter has no pages", sourcekit.ErrNotFound)
	}
	return pages, nil
}

func (m *mangataro) headers(post bool) map[string]string {
	h := map[string]string{"Referer": m.base + "/"}
	if post {
		h["Content-Type"] = "application/json"
	}
	return h
}

// mangataroSlug reads the slug out of "/manga/<slug>" or "/read/<slug>/<chapter>".
func mangataroSlug(raw string) string {
	var parts []string
	for _, p := range strings.Split(sourcekit.Path(raw), "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if (len(parts) == 2 && parts[0] == "manga") || (len(parts) == 3 && parts[0] == "read") {
		return parts[1]
	}
	return ""
}

// mangataroUnescape undoes the site's HTML escaping, which is sometimes
// applied twice ("&amp;#8217;").
func mangataroUnescape(s string) string {
	for {
		u := html.UnescapeString(s)
		if u == s {
			return s
		}
		s = u
	}
}
