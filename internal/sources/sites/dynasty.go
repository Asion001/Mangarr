package sites

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// Dynasty Reader (dynasty-scans.com) serves JSON for every page when ".json"
// is added to its address, so this reads that. A library entry is a series,
// an anthology, a doujin, an issue, or a lone chapter that belongs to none of
// those. The id is set in the extension's build file.
const dynID = "669095474988166464"

const dynSite = "https://dynasty-scans.com"

func init() {
	sourcekit.Register(dynID, func(d sourcekit.Deps) sourcekit.Site {
		return &dynasty{c: d.Client, base: dynSite, fetchLimit: 2}
	})
}

type dynasty struct {
	c *sourcekit.Client
	// base is the site's address (tests point it at a recorded copy).
	base string
	// fetchLimit is how many pages of a long chapter list to read (0: all).
	fetchLimit int
}

func (s *dynasty) Info() sourcekit.Info {
	return sourcekit.Info{ID: dynID, Name: "Dynasty Scans", Lang: "en", BaseURL: s.base, SupportsBrowse: true,
		IconURL: s.base + "/favicon.ico"}
}

// Politeness: the extension allows one request a second.
func (s *dynasty) Politeness() sourcekit.Politeness {
	return sourcekit.Politeness{RequestsPerMinute: 60, MaxConcurrent: 1}
}

func (s *dynasty) Options() []sourcekit.Option {
	value := "all"
	if s.fetchLimit > 0 {
		value = strconv.Itoa(s.fetchLimit)
	}
	return []sourcekit.Option{{Key: "chapterFetchLimit", Title: "Chapter list pages", Type: "select", Value: value,
		Help: "How many pages of an entry's chapter list to read. Mostly matters for doujins; more pages load slower.",
		Choices: []sourcekit.Choice{{Value: "2", Label: "2 pages"}, {Value: "5", Label: "5 pages"},
			{Value: "10", Label: "10 pages"}, {Value: "all", Label: "All pages"}}}}
}

func (s *dynasty) SetOption(key string, value any) error {
	if key != "chapterFetchLimit" {
		return fmt.Errorf("%w: no option %q", sourcekit.ErrUnsupported, key)
	}
	v := fmt.Sprint(value)
	if v == "all" {
		s.fetchLimit = 0
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return fmt.Errorf("%v is not a page count", value)
	}
	s.fetchLimit = n
	return nil
}

func (s *dynasty) headers() map[string]string {
	return map[string]string{"Referer": s.base + "/", "Origin": s.base}
}

// ---- the site's shapes (only the fields we use) -----------------------------

const (
	dynSeries    = "Series"
	dynChapter   = "Chapter"
	dynAnthology = "Anthology"
	dynDoujin    = "Doujin"
	dynIssue     = "Issue"
)

// dynDirs is the directory each kind of entry lives in.
var dynDirs = map[string]string{dynSeries: "series", dynAnthology: "anthologies", dynDoujin: "doujins", dynIssue: "issues"}

type dynTag struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	Permalink string `json:"permalink"`
}

type dynBrowse struct {
	Chapters []struct {
		Title     string   `json:"title"`
		Permalink string   `json:"permalink"`
		Tags      []dynTag `json:"tags"`
	} `json:"chapters"`
	CurrentPage int `json:"current_page"`
	TotalPages  int `json:"total_pages"`
}

// dynTagging is a row of an entry's chapter list: a header ("Volume 2") or a
// chapter.
type dynTagging struct {
	Header     *string  `json:"header"`
	Title      string   `json:"title"`
	Permalink  string   `json:"permalink"`
	ReleasedOn string   `json:"released_on"`
	Tags       []dynTag `json:"tags"`
}

type dynManga struct {
	Name        string       `json:"name"`
	Type        string       `json:"type"`
	Permalink   string       `json:"permalink"`
	Tags        []dynTag     `json:"tags"`
	Cover       *string      `json:"cover"`
	Description *string      `json:"description"`
	Aliases     []string     `json:"aliases"`
	Taggings    []dynTagging `json:"taggings"`
	TotalPages  int          `json:"total_pages"`
}

type dynChapterJSON struct {
	Title      string   `json:"title"`
	Permalink  string   `json:"permalink"`
	Tags       []dynTag `json:"tags"`
	ReleasedOn string   `json:"released_on"`
	Pages      []struct {
		URL string `json:"url"`
	} `json:"pages"`
}

// ---- covers -----------------------------------------------------------------

// dynCoversJSON is the extension's map of covers by directory and permalink:
// the site's lists carry none.
//
//go:embed dynasty_covers.json
var dynCoversJSON []byte

var (
	dynCoversOnce sync.Once
	dynCovers     map[string]map[string]string
)

// cachedCover is an entry's cover from the extension's map. A lone chapter's
// cover is its first page, which takes a request; lists leave it out.
func (s *dynasty) cachedCover(dir, permalink string) string {
	dynCoversOnce.Do(func() { _ = json.Unmarshal(dynCoversJSON, &dynCovers) })
	if file := dynCovers[dir][permalink]; file != "" {
		return s.coverURL(file)
	}
	return ""
}

// coverURL is where a cover file is served ("/023/811/original/a.jpg" lives
// under /system/tag_contents_covers/000/).
func (s *dynasty) coverURL(file string) string {
	u, err := url.Parse(s.base + file)
	if err != nil {
		return ""
	}
	p := strings.TrimPrefix(u.EscapedPath(), "/")
	if !strings.HasPrefix(p, "system/") {
		p = "system/tag_contents_covers/000/" + p
	}
	return s.base + "/" + p
}

// dynCoverExts are the files an original-size cover may be.
var dynCoverExts = []string{"jpg", "jpeg", "png", "webp", "jfif", "gif", "JPG", "JPEG", "PNG", "WEBP", "JFIF", "GIF"}

// dynSwapCover is a cover address with its size folder and file extension
// changed (".../000/023/811/medium/a.jpg" → ".../original/a.png").
func dynSwapCover(cover, size, ext string) (string, bool) {
	u, err := url.Parse(cover)
	if err != nil {
		return "", false
	}
	segs := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	if len(segs) != 7 {
		return "", false
	}
	segs[5] = size
	last := segs[6]
	if i := strings.LastIndex(last, "."); i >= 0 {
		last = last[:i]
	}
	segs[6] = last + "." + ext
	return u.Scheme + "://" + u.Host + "/" + strings.Join(segs, "/"), true
}

// hdCover finds the original of a medium-size cover, when the site has one.
func (s *dynasty) hdCover(ctx context.Context, cover string) string {
	u, err := url.Parse(cover)
	if err != nil {
		return cover
	}
	if segs := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/"); len(segs) != 7 || segs[5] != "medium" {
		return cover
	}
	for _, ext := range dynCoverExts {
		hd, _ := dynSwapCover(cover, "original", ext)
		if _, err := s.c.Do(ctx, sourcekit.Request{Method: http.MethodHead, URL: hd, Headers: s.headers()}); err == nil {
			return hd
		}
	}
	return cover
}

// thumbnail picks an entry's best cover: the cached one when it is the same
// file as the site's own (saving the lookups for an original), else the
// site's, at its original size when there is one.
func (s *dynasty) thumbnail(ctx context.Context, m *dynManga) string {
	fresh := ""
	if m.Cover != nil && *m.Cover != "" {
		fresh = s.coverURL(*m.Cover)
	}
	cached := s.cachedCover(dynDirs[m.Type], m.Permalink)
	if fresh == "" || cached == "" {
		if fresh != "" {
			return s.hdCover(ctx, fresh)
		}
		return cached
	}
	if sd, ok := dynSwapCover(cached, "medium", "jpg"); ok && sd == fresh {
		return cached
	}
	return s.hdCover(ctx, fresh)
}

// ---- lists ------------------------------------------------------------------

var dynChapterSlugRe = regexp.MustCompile(`(.*?)_(ch[0-9_]+|volume_[0-9_\w]+)`)

// resolve points a chapter's link at the series it belongs to, when its
// permalink says which ("a_series_ch01" → series "a_series").
func dynResolve(dir, permalink string) (string, string) {
	if dir != "chapters" {
		return dir, permalink
	}
	if g := dynChapterSlugRe.FindStringSubmatch(permalink); g != nil {
		return "series", g[1]
	}
	return dir, permalink
}

// dynTitle makes a title of a permalink ("a_series" → "A Series").
func dynTitle(permalink string) string {
	words := strings.Split(permalink, "_")
	for i, w := range words {
		if w != "" {
			r := []rune(w)
			words[i] = strings.ToUpper(string(r[0])) + string(r[1:])
		}
	}
	return strings.Join(words, " ")
}

func (s *dynasty) add(res *sourcekit.Results, dir, permalink, title string) {
	path := "/" + dir + "/" + permalink
	if permalink == "" || res.Has(path) {
		return
	}
	res.Mangas = append(res.Mangas, sourcekit.Manga{URL: path, Title: title, CoverURL: s.cachedCover(dir, permalink)})
}

// Popular is the home page's most popular of the past week, then (from page
// 2) the chapters added lately, as in the extension.
func (s *dynasty) Popular(ctx context.Context, page int) (sourcekit.Results, error) {
	if page > 1 {
		return s.added(ctx, page-1)
	}
	h := s.headers()
	h["Accept"] = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
	doc, err := s.c.Document(ctx, sourcekit.Request{URL: s.base, Headers: h})
	if err != nil {
		return sourcekit.Results{}, err
	}
	res := sourcekit.Results{HasNext: true}
	doc.Find(`h4:contains("Most Popular of Past 7 Days") ~ ul.cover-list a.thumbnail`).Each(func(_ int, a *goquery.Selection) {
		segs := strings.Split(strings.Trim(sourcekit.Path(sourcekit.Abs(s.base, a.AttrOr("href", ""))), "/"), "/")
		if len(segs) < 2 {
			return
		}
		dir, permalink := dynResolve("chapters", segs[1])
		s.add(&res, dir, permalink, dynTitle(permalink))
	})
	return res, nil
}

// Latest lists the chapters added lately. (The extension has no latest
// listing; this is the one its popular listing continues with.)
func (s *dynasty) Latest(ctx context.Context, page int) (sourcekit.Results, error) {
	if page < 1 {
		page = 1
	}
	return s.added(ctx, page)
}

func (s *dynasty) added(ctx context.Context, page int) (sourcekit.Results, error) {
	var out dynBrowse
	req := sourcekit.Request{URL: s.base + "/chapters/added.json", Query: url.Values{"page": {strconv.Itoa(page)}}, Headers: s.headers()}
	if err := s.c.JSON(ctx, req, &out); err != nil {
		return sourcekit.Results{}, err
	}
	res := sourcekit.Results{HasNext: out.CurrentPage < out.TotalPages}
	for _, c := range out.Chapters {
		inSeries := false
		for _, t := range c.Tags {
			if dir, ok := dynDirs[t.Type]; ok {
				s.add(&res, dir, t.Permalink, t.Name)
				inSeries = inSeries || t.Type == dynSeries
			}
		}
		// a chapter of no series (mostly doujins) is an entry of its own
		if !inSeries {
			s.add(&res, "chapters", c.Permalink, c.Title)
		}
	}
	return res, nil
}

var (
	dynEntryRe  = regexp.MustCompile(`/(series|anthologies|chapters|doujins|issues)/`)
	dynDoujinRe = regexp.MustCompile(`/doujins/`)
)

// Search asks for every kind of entry, sorted by relevance, as the
// extension's default filters do. Chapters of a series are listed as their
// series.
func (s *dynasty) Search(ctx context.Context, query string, page int) (sourcekit.Results, error) {
	query = strings.TrimSpace(query)
	q := url.Values{"q": {query}, "sort": {""}}
	if query == "" {
		q.Set("sort", "released_on")
	}
	for _, t := range []string{dynSeries, dynChapter, dynAnthology, dynDoujin, dynIssue} {
		q.Add("classes[]", t)
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	doc, err := s.c.Document(ctx, sourcekit.Request{URL: s.base + "/search", Query: q, Headers: s.headers()})
	if err != nil {
		return sourcekit.Results{}, err
	}
	var res sourcekit.Results
	doc.Find(".chapter-list a.name, .chapter-list .doujin_tags a").Each(func(_ int, a *goquery.Selection) {
		href := sourcekit.Abs(s.base, a.AttrOr("href", ""))
		if a.HasClass("name") && !dynEntryRe.MatchString(href) || !a.HasClass("name") && !dynDoujinRe.MatchString(href) {
			return
		}
		segs := strings.Split(strings.Trim(sourcekit.Path(href), "/"), "/")
		if len(segs) < 2 {
			return
		}
		dir, permalink := dynResolve(segs[0], segs[1])
		title := mboxOwnText(a)
		if dir != segs[0] {
			title = dynTitle(permalink)
		}
		s.add(&res, dir, permalink, title)
	})
	res.HasNext = doc.Find(".pagination [rel=next]").Length() > 0
	return res, nil
}

// ---- one entry --------------------------------------------------------------

// entry splits "/<dir>/<permalink>".
func dynEntry(raw string) (string, string, error) {
	segs := strings.Split(strings.Trim(sourcekit.Path(raw), "/"), "/")
	if len(segs) == 2 && segs[1] != "" {
		switch segs[0] {
		case "series", "anthologies", "doujins", "issues", "chapters":
			return segs[0], segs[1], nil
		}
	}
	return "", "", fmt.Errorf("%q is not a Dynasty Scans entry (migrate it to update its link)", raw)
}

func (s *dynasty) entryJSON(ctx context.Context, dir, permalink string, page int, out any) error {
	req := sourcekit.Request{URL: s.base + "/" + dir + "/" + url.PathEscape(permalink) + ".json", Headers: s.headers()}
	if page > 1 {
		req.Query = url.Values{"page": {strconv.Itoa(page)}}
	}
	return notFound(s.c.JSON(ctx, req, out))
}

func (s *dynasty) Details(ctx context.Context, ref sourcekit.Ref) (sourcekit.Details, error) {
	dir, permalink, err := dynEntry(ref.URL)
	if err != nil {
		return sourcekit.Details{}, err
	}
	path := "/" + dir + "/" + permalink
	if dir == "chapters" {
		var c dynChapterJSON
		if err := s.entryJSON(ctx, dir, permalink, 1, &c); err != nil {
			return sourcekit.Details{}, err
		}
		d := s.chapterDetails(&c)
		d.URL, d.WebURL = path, s.base+path
		return d, nil
	}
	var m dynManga
	if err := s.entryJSON(ctx, dir, permalink, 1, &m); err != nil {
		return sourcekit.Details{}, err
	}
	d := s.mangaDetails(&m)
	d.URL, d.WebURL = path, s.base+path
	d.CoverURL = s.thumbnail(ctx, &m)
	return d, nil
}

// dynAuthorLimit is how many authors the author field names; the rest go in
// the description.
const dynAuthorLimit = 15

// dynGroups keeps "type: value" pairs grouped by type in the order met.
type dynGroups struct {
	order []string
	vals  map[string][]string
	seen  map[string]bool
}

func (g *dynGroups) add(typ, val string) {
	if g.vals == nil {
		g.vals, g.seen = map[string][]string{}, map[string]bool{}
	}
	if g.seen[typ+"\x00"+val] {
		return
	}
	g.seen[typ+"\x00"+val] = true
	if _, ok := g.vals[typ]; !ok {
		g.order = append(g.order, typ)
	}
	g.vals[typ] = append(g.vals[typ], val)
}

func (g *dynGroups) write(b *strings.Builder) {
	for _, typ := range g.order {
		b.WriteString(typ + ":\n")
		for _, v := range g.vals[typ] {
			b.WriteString("• " + v + "\n")
		}
		b.WriteString("\n")
	}
}

// dynSet is an ordered set of strings.
type dynSet struct {
	list []string
	seen map[string]bool
}

func (s *dynSet) add(v string) {
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	if !s.seen[v] {
		s.seen[v] = true
		s.list = append(s.list, v)
	}
}

var dynUnicodeRe = regexp.MustCompile(`\\u([0-9A-Fa-f]{4})`)

func (s *dynasty) mangaDetails(m *dynManga) sourcekit.Details {
	var authors, tags, status dynSet
	var others dynGroups
	for _, t := range m.Tags {
		switch t.Type {
		case "Author":
			authors.add(t.Name)
		case "General":
			tags.add(t.Name)
		case "Status":
			status.add(t.Name)
			others.add(t.Type, t.Name)
		default:
			others.add(t.Type, t.Name)
		}
	}
	for _, c := range m.Taggings {
		if c.Header != nil {
			continue
		}
		for _, t := range c.Tags {
			switch t.Type {
			case "Author":
				authors.add(t.Name)
			case "General":
				tags.add(t.Name)
			case dynSeries, dynDoujin, dynAnthology, dynIssue, "Scanlator":
			default:
				others.add(t.Type, t.Name)
			}
		}
	}
	d := sourcekit.Details{Manga: sourcekit.Manga{Title: strings.TrimSpace(m.Name)}, Genres: tags.list,
		Status: sourcekit.StatusUnknown}
	if len(authors.list) > dynAuthorLimit {
		d.Author = strings.Join(authors.list[:dynAuthorLimit], ", ") + "..."
	} else {
		d.Author = strings.Join(authors.list, ", ")
	}
	d.Artist = d.Author

	var b strings.Builder
	if limit := s.fetchLimit; limit > 0 && limit < m.TotalPages {
		fmt.Fprintf(&b, "IMPORTANT: Only the first %d pages of the chapter list are read. You can change this in the catalog's settings.\n\n", limit)
	}
	if m.Description != nil {
		raw := dynUnicodeRe.ReplaceAllStringFunc(*m.Description, func(x string) string {
			n, err := strconv.ParseUint(x[2:], 16, 32)
			if err != nil {
				return x
			}
			return string(rune(n))
		})
		if frag, err := goquery.NewDocumentFromReader(strings.NewReader("<body>" + raw + "</body>")); err == nil {
			frag.Find("a").Remove()
			b.WriteString(strings.TrimSpace(frag.Find("body").Text()))
			b.WriteString("\n\n")
		}
	}
	b.WriteString("Type: " + m.Type + "\n\n")
	if len(authors.list) > dynAuthorLimit {
		for _, a := range authors.list {
			others.add("Author", a)
		}
	}
	others.write(&b)
	if len(m.Aliases) > 0 {
		b.WriteString("Aliases:\n")
		for _, a := range m.Aliases {
			b.WriteString("• " + a + "\n")
		}
	}
	d.Description = strings.TrimSpace(b.String())

	has := func(v string) bool { return status.seen[v] }
	switch {
	case has("Ongoing"):
		d.Status = sourcekit.StatusOngoing
	case has("Completed"):
		d.Status = sourcekit.StatusCompleted
	case has("On Hiatus"):
		d.Status = sourcekit.StatusHiatus
	case has("Licensed"):
		// licensed and taken down: mangarr has no such status
	case has("Dropped"), has("Cancelled"), has("Not Updated"), has("Abandoned"), has("Removed"):
		d.Status = sourcekit.StatusCancelled
	}
	return d
}

// chapterDetails describes a lone chapter kept as an entry of its own.
func (s *dynasty) chapterDetails(c *dynChapterJSON) sourcekit.Details {
	var authors, tags dynSet
	var others dynGroups
	for _, t := range c.Tags {
		switch t.Type {
		case "Author":
			authors.add(t.Name)
		case "General":
			tags.add(t.Name)
		default:
			others.add(t.Type, t.Name)
		}
	}
	d := sourcekit.Details{Manga: sourcekit.Manga{Title: strings.TrimSpace(c.Title)}, Author: strings.Join(authors.list, ", "),
		Genres: tags.list, Status: sourcekit.StatusCompleted}
	d.Artist = d.Author
	var b strings.Builder
	b.WriteString("Type: " + dynChapter + "\n\n")
	others.write(&b)
	b.WriteString("Released: " + c.ReleasedOn)
	d.Description = strings.TrimSpace(b.String())
	if len(c.Pages) > 0 {
		d.CoverURL = s.coverURL(c.Pages[0].URL)
	}
	one := 1
	d.Chapters = &one
	return d
}

func (s *dynasty) Chapters(ctx context.Context, ref sourcekit.Ref) ([]sourcekit.Chapter, error) {
	dir, permalink, err := dynEntry(ref.URL)
	if err != nil {
		return nil, err
	}
	if dir == "chapters" {
		// a lone chapter is its own only chapter
		var c dynChapterJSON
		if err := s.entryJSON(ctx, dir, permalink, 1, &c); err != nil {
			return nil, err
		}
		ch := sourcekit.Chapter{URL: "/chapters/" + c.Permalink, Name: "Chapter", Number: -1,
			Scanlator: dynScanlators(c.Tags), WebURL: s.base + "/chapters/" + c.Permalink}
		ch.UploadedAt = dynDate(c.ReleasedOn)
		return []sourcekit.Chapter{ch}, nil
	}
	var m dynManga
	if err := s.entryJSON(ctx, dir, permalink, 1, &m); err != nil {
		return nil, err
	}
	rows := m.Taggings
	for page := 2; page <= m.TotalPages && (s.fetchLimit == 0 || page <= s.fetchLimit); page++ {
		var more dynManga
		if err := s.entryJSON(ctx, dir, permalink, page, &more); err != nil {
			return nil, err
		}
		rows = append(rows, more.Taggings...)
	}
	chapters := dynChapterList(m.Type, rows)
	for i := range chapters {
		chapters[i].WebURL = s.base + chapters[i].URL
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("%w: no chapters listed", sourcekit.ErrNotFound)
	}
	return chapters, nil
}

// dynChapterList reads an entry's chapter rows: a header names the chapters
// under it, and chapters of anything but a series are credited to their
// authors. The site lists oldest first, except for doujins.
func dynChapterList(typ string, rows []dynTagging) []sourcekit.Chapter {
	var header *string
	var out []sourcekit.Chapter
	for _, r := range rows {
		if r.Header != nil {
			header = r.Header
			continue
		}
		name := r.Title
		if header != nil {
			name = *header + " " + r.Title
		}
		if typ != dynSeries {
			var authors []string
			for _, t := range r.Tags {
				if t.Type == "Author" {
					authors = append(authors, t.Name)
				}
			}
			if len(authors) > 0 {
				name += " by " + strings.Join(authors, " and ")
			}
		}
		out = append(out, sourcekit.Chapter{URL: "/chapters/" + r.Permalink, Name: strings.TrimSpace(name),
			Number: chapterNumber(r.Title), Scanlator: dynScanlators(r.Tags), UploadedAt: dynDate(r.ReleasedOn)})
	}
	if typ != dynDoujin {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out
}

func dynScanlators(tags []dynTag) string {
	var names []string
	for _, t := range tags {
		if t.Type == "Scanlator" {
			names = append(names, t.Name)
		}
	}
	return strings.Join(names, ", ")
}

func dynDate(s string) *time.Time {
	t, err := time.ParseInLocation("2006-01-02", s, time.UTC)
	if err != nil {
		return nil
	}
	return &t
}

func (s *dynasty) Pages(ctx context.Context, ch sourcekit.PageRef) ([]sourcekit.PageImage, error) {
	dir, permalink, err := dynEntry(ch.URL)
	if err != nil || dir != "chapters" {
		return nil, fmt.Errorf("%q is not a chapter url (refresh the chapter list)", ch.URL)
	}
	var c dynChapterJSON
	if err := s.entryJSON(ctx, dir, permalink, 1, &c); err != nil {
		return nil, err
	}
	pages := make([]sourcekit.PageImage, 0, len(c.Pages))
	for _, p := range c.Pages {
		if p.URL != "" {
			pages = append(pages, sourcekit.PageImage{Index: len(pages), URL: sourcekit.Abs(s.base, p.URL),
				Headers: map[string]string{"Referer": s.base + "/"}})
		}
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: the chapter has no pages", sourcekit.ErrNotFound)
	}
	return pages, nil
}
