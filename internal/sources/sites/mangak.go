package sites

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// MangaK is the engine behind a family of Next.js manga sites (Keiyoushi's
// "mangak" theme): lists and chapters come from a JSON API on the "api."
// subdomain, while a manga's details and a chapter's images are only in the
// data the site's pages hydrate from. One engine serves every site on the
// theme; a site is an mkSite with its own name and address.

// mkSites are the sites on the theme. The ids are the Keiyoushi ones (set
// explicitly in each extension's build file), so Mihon backups link here.
var mkSites = []mkSite{
	// MangaK is the old MangaBuddy extension, hence its id.
	{id: "5020395055978987501", name: "MangaK", base: "https://mangak.io"},
}

func init() {
	for _, s := range mkSites {
		sourcekit.Register(s.id, func(d sourcekit.Deps) sourcekit.Site {
			m := s
			m.c = d.Client
			if m.api == "" {
				m.api = mkAPIFor(m.base)
			}
			return &m
		})
	}
}

// mkAPIFor is the theme's API address for a site: "api." + the site's host.
func mkAPIFor(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	return "https://api." + u.Hostname()
}

const (
	// mkPageSize is what the extension asks for.
	mkPageSize = 24
	// mkQueryLimit is the longest search the API accepts.
	mkQueryLimit = 50
)

type mkSite struct {
	c    *sourcekit.Client
	id   string
	name string
	// base and api are the addresses to talk to (tests point them at a
	// recorded copy).
	base, api string
	nsfw      bool
}

func (m *mkSite) Info() sourcekit.Info {
	return sourcekit.Info{ID: m.id, Name: m.name, Lang: "en", BaseURL: m.base, NSFW: m.nsfw, SupportsBrowse: true,
		IconURL: m.base + "/favicon.ico"}
}

// Politeness: the extension sets no limit of its own, so stay gentle.
func (m *mkSite) Politeness() sourcekit.Politeness {
	return sourcekit.Politeness{RequestsPerMinute: 60, MaxConcurrent: 2}
}

func (m *mkSite) headers() map[string]string {
	return map[string]string{"Referer": m.base + "/", "Origin": m.base}
}

// ---- the API's shapes (only the fields we use) ------------------------------

type mkSearchDTO struct {
	Data struct {
		Items []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Cover string `json:"cover"`
			URL   string `json:"url"`
		} `json:"items"`
		Pagination struct {
			HasNext bool `json:"has_next"`
		} `json:"pagination"`
	} `json:"data"`
}

type mkChaptersDTO struct {
	Data struct {
		Chapters []struct {
			URL           string   `json:"url"`
			Name          string   `json:"name"`
			UpdatedAt     string   `json:"updated_at"`
			ChapterNumber *float64 `json:"chapter_number"`
		} `json:"chapters"`
	} `json:"data"`
}

// mkPageProps is what a page hydrates from: the manga on a manga page, the
// images on a chapter page.
type mkPageProps struct {
	PageProps struct {
		InitialManga *struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Summary string `json:"summary"`
			Status  string `json:"status"`
			Cover   string `json:"cover"`
			URL     string `json:"url"`
			Authors []struct {
				Name string `json:"name"`
			} `json:"authors"`
			Genres []struct {
				Name string `json:"name"`
			} `json:"genres"`
		} `json:"initialManga"`
		InitialChapter *struct {
			Images []string `json:"images"`
		} `json:"initialChapter"`
	} `json:"pageProps"`
}

// ---- lists ------------------------------------------------------------------

func (m *mkSite) Search(ctx context.Context, query string, page int) (sourcekit.Results, error) {
	q := url.Values{}
	if s := mkQuery(query); s != "" {
		q.Set("q", s)
	}
	return m.list(ctx, page, q)
}

func (m *mkSite) Popular(ctx context.Context, page int) (sourcekit.Results, error) {
	return m.list(ctx, page, url.Values{"sort": {"popular"}, "window": {"week"}})
}

func (m *mkSite) Latest(ctx context.Context, page int) (sourcekit.Results, error) {
	return m.list(ctx, page, url.Values{"sort": {"latest"}})
}

// mkQuery is a search as the API takes it: letters, digits and spaces only,
// and at most 50 of them.
func mkQuery(query string) string {
	kept := []rune{}
	for _, r := range query {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' {
			kept = append(kept, r)
		}
	}
	s := []rune(strings.TrimSpace(string(kept)))
	if len(s) > mkQueryLimit {
		s = s[:mkQueryLimit]
	}
	return string(s)
}

func (m *mkSite) list(ctx context.Context, page int, q url.Values) (sourcekit.Results, error) {
	if page < 1 {
		page = 1
	}
	q.Set("page", strconv.Itoa(page))
	q.Set("limit", strconv.Itoa(mkPageSize))
	var out mkSearchDTO
	if err := m.c.JSON(ctx, sourcekit.Request{URL: m.api + "/titles/search", Query: q, Headers: m.headers()}, &out); err != nil {
		return sourcekit.Results{}, err
	}
	res := sourcekit.Results{HasNext: out.Data.Pagination.HasNext}
	for _, it := range out.Data.Items {
		path := m.path(it.URL)
		if path == "" || res.Has(path) {
			continue
		}
		res.Mangas = append(res.Mangas, sourcekit.Manga{URL: path, ID: it.ID, Title: strings.TrimSpace(it.Name),
			CoverURL: sourcekit.Abs(m.base, it.Cover)})
	}
	return res, nil
}

// path is a manga or chapter link as mangarr stores it.
func (m *mkSite) path(raw string) string {
	if i := strings.Index(raw, "#"); i >= 0 {
		raw = raw[:i]
	}
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	return sourcekit.Path(sourcekit.Abs(m.base+"/", raw))
}

// ---- one manga --------------------------------------------------------------

// props reads the data a page of the site hydrates from.
func (m *mkSite) props(ctx context.Context, path string) (*mkPageProps, error) {
	doc, err := m.c.Document(ctx, sourcekit.Request{URL: m.base + path, Headers: m.headers()})
	if err != nil {
		return nil, notFound(err)
	}
	var out mkPageProps
	found, err := mkNextData(doc, "pageProps", &out)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if !found {
		return nil, fmt.Errorf("%s: could not find the page's Next.js data", path)
	}
	return &out, nil
}

func (m *mkSite) Details(ctx context.Context, ref sourcekit.Ref) (sourcekit.Details, error) {
	path := m.path(ref.URL)
	if path == "" {
		return sourcekit.Details{}, fmt.Errorf("%q is not a manga url", ref.URL)
	}
	p, err := m.props(ctx, path)
	if err != nil {
		return sourcekit.Details{}, err
	}
	x := p.PageProps.InitialManga
	if x == nil {
		return sourcekit.Details{}, fmt.Errorf("%w: no manga at %s", sourcekit.ErrNotFound, path)
	}
	d := sourcekit.Details{Manga: sourcekit.Manga{URL: path, ID: x.ID, Title: strings.TrimSpace(x.Name),
		CoverURL: sourcekit.Abs(m.base, x.Cover)}, Description: strings.TrimSpace(x.Summary),
		Status: mkStatus(x.Status), WebURL: m.base + path}
	var authors []string
	for _, a := range x.Authors {
		authors = append(authors, a.Name)
	}
	d.Author = strings.Join(authors, ", ")
	for _, g := range x.Genres {
		d.Genres = append(d.Genres, g.Name)
	}
	return d, nil
}

func mkStatus(s string) string {
	switch strings.ToLower(s) {
	case "ongoing":
		return sourcekit.StatusOngoing
	case "completed":
		return sourcekit.StatusCompleted
	case "hiatus":
		return sourcekit.StatusHiatus
	case "cancelled":
		return sourcekit.StatusCancelled
	}
	return sourcekit.StatusUnknown
}

func (m *mkSite) Chapters(ctx context.Context, ref sourcekit.Ref) ([]sourcekit.Chapter, error) {
	id := ref.ID
	if id == "" {
		// a manga without its API id (a backup import): the page has it
		d, err := m.Details(ctx, ref)
		if err != nil {
			return nil, err
		}
		id = d.ID
	}
	if id == "" {
		return nil, fmt.Errorf("%q: could not find the manga's id", ref.URL)
	}
	var out mkChaptersDTO
	// cv busts the API's cache, as the site itself does
	q := url.Values{"cv": {strconv.FormatInt(time.Now().UnixMilli(), 10)}}
	req := sourcekit.Request{URL: m.api + "/titles/" + url.PathEscape(id) + "/chapters", Query: q, Headers: m.headers()}
	if err := m.c.JSON(ctx, req, &out); err != nil {
		return nil, notFound(err)
	}
	rows := out.Data.Chapters
	num := func(i int) float64 {
		if n := rows[i].ChapterNumber; n != nil {
			return *n
		}
		return 0
	}
	idx := make([]int, len(rows))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return num(idx[a]) > num(idx[b]) })
	chapters := make([]sourcekit.Chapter, 0, len(rows))
	for _, i := range idx {
		c := rows[i]
		path := m.path(c.URL)
		if path == "" {
			continue
		}
		ch := sourcekit.Chapter{URL: path, Name: strings.TrimSpace(c.Name), Number: -1, WebURL: m.base + path}
		if c.ChapterNumber != nil {
			ch.Number = *c.ChapterNumber
		} else {
			ch.Number = chapterNumber(ch.Name)
		}
		if t, err := time.Parse(time.RFC3339, c.UpdatedAt); err == nil {
			ch.UploadedAt = &t
		}
		chapters = append(chapters, ch)
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("%w: no chapters listed", sourcekit.ErrNotFound)
	}
	return chapters, nil
}

// Pages come from the chapter page's own data. The extension retries images
// from one CDN (rx.qvzr?.org) on a second host when the first fails; mangarr
// has no per-page fallback, so the first address is what it gets.
func (m *mkSite) Pages(ctx context.Context, ch sourcekit.PageRef) ([]sourcekit.PageImage, error) {
	path := m.path(ch.URL)
	if path == "" {
		return nil, fmt.Errorf("%q is not a chapter url", ch.URL)
	}
	p, err := m.props(ctx, path)
	if err != nil {
		return nil, err
	}
	var pages []sourcekit.PageImage
	if c := p.PageProps.InitialChapter; c != nil {
		for _, img := range c.Images {
			if img = strings.TrimSpace(img); img != "" {
				pages = append(pages, sourcekit.PageImage{Index: len(pages), URL: sourcekit.Abs(m.base, img),
					Headers: map[string]string{"Referer": m.base + "/"}})
			}
		}
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: the chapter has no pages", sourcekit.ErrNotFound)
	}
	return pages, nil
}

// ---- Next.js data -------------------------------------------------------------

// mkNextData finds, in a Next.js page, the first JSON object holding key and
// decodes it into out. It reads what both kinds of Next.js page hydrate from:
// the App Router's React Flight rows (self.__next_f.push) and, failing those,
// the Pages Router's __NEXT_DATA__ script. It ports Keiyoushi's
// extractNextJs, including the references Flight uses to share values
// between rows ("$1", "$L2", "$3:props:children").
func mkNextData(doc *goquery.Document, key string, out any) (bool, error) {
	for _, payload := range mkNextPayloads(doc) {
		if v := mkFindKey(payload, key); v != nil {
			raw, err := json.Marshal(v)
			if err != nil {
				return false, err
			}
			return true, json.Unmarshal(raw, out)
		}
	}
	return false, nil
}

var mkNextFRe = regexp.MustCompile(`(?s)self\.__next_f\.push\(\s*(\[.*])\s*\)\s*;?\s*$`)

func mkNextPayloads(doc *goquery.Document) []any {
	var flight strings.Builder
	doc.Find("script:not([src])").Each(func(_ int, s *goquery.Selection) {
		data := strings.TrimSpace(s.Text())
		if !strings.Contains(data, "self.__next_f.push") {
			return
		}
		g := mkNextFRe.FindStringSubmatch(data)
		if g == nil {
			return
		}
		var arr []any
		if json.Unmarshal([]byte(g[1]), &arr) != nil || len(arr) < 2 {
			return
		}
		if chunk, ok := arr[1].(string); ok {
			flight.WriteString(chunk)
		}
	})
	if flight.Len() > 0 {
		f := mkParseFlight(flight.String())
		out := make([]any, 0, len(f.rows))
		for _, r := range f.rows {
			out = append(out, f.resolve(r, nil))
		}
		if len(out) > 0 {
			return out
		}
	}
	raw := doc.Find("script#__NEXT_DATA__").First().Text()
	if raw == "" {
		return nil
	}
	var root any
	if json.Unmarshal([]byte(raw), &root) != nil {
		return nil
	}
	var out []any
	if r, ok := root.(map[string]any); ok {
		if props, ok := r["props"].(map[string]any); ok && props["pageProps"] != nil {
			out = append(out, props["pageProps"])
		}
	}
	return append(out, root)
}

// mkFindKey is the first object (depth first) that has key.
func mkFindKey(v any, key string) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if _, ok := x[key]; ok {
			return x
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys) // maps have no order; be deterministic at least
		for _, k := range keys {
			if m := mkFindKey(x[k], key); m != nil {
				return m
			}
		}
	case []any:
		for _, e := range x {
			if m := mkFindKey(e, key); m != nil {
				return m
			}
		}
	}
	return nil
}

// mkFlight is a parsed React Flight stream: its JSON rows in order, and the
// rows and text chunks other rows refer to by their hex id.
type mkFlight struct {
	rows   []any
	models map[string]any
	texts  map[string]string
}

func mkParseFlight(body string) *mkFlight {
	f := &mkFlight{models: map[string]any{}, texts: map[string]string{}}
	pos := 0
	for pos < len(body) {
		colon := strings.IndexByte(body[pos:], ':')
		if colon < 0 {
			break
		}
		colon += pos
		id := body[pos:colon]
		if !mkIsHex(id) {
			pos++
			continue
		}
		pos = colon + 1
		if pos >= len(body) {
			break
		}
		if body[pos] == 'T' {
			// a text chunk: T<byte length in hex>,<text>
			comma := strings.IndexByte(body[pos:], ',')
			if comma < 0 {
				break
			}
			n, err := strconv.ParseInt(body[pos+1:pos+comma], 16, 64)
			if err != nil {
				break
			}
			start := pos + comma + 1
			end := start + int(n)
			if end > len(body) {
				end = len(body)
			}
			f.texts[id] = body[start:end]
			var v any
			if json.Unmarshal([]byte(body[start:end]), &v) == nil {
				f.rows = append(f.rows, v)
			}
			pos = end
			continue
		}
		dec := json.NewDecoder(strings.NewReader(body[pos:]))
		var v any
		if err := dec.Decode(&v); err != nil {
			// not JSON (an import, a hint): skip the row
			nl := strings.IndexByte(body[pos:], '\n')
			if nl < 0 {
				break
			}
			pos += nl + 1
			continue
		}
		f.rows = append(f.rows, v)
		f.models[id] = v
		pos += int(dec.InputOffset())
	}
	return f
}

func mkIsHex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

// resolve replaces Flight's special strings with what they stand for.
func (f *mkFlight) resolve(v any, resolving map[string]bool) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = f.resolve(e, resolving)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = f.resolve(e, resolving)
		}
		return out
	case string:
		if len(x) < 2 || x[0] != '$' {
			return x
		}
		switch {
		case x == "$undefined":
			return nil
		case x == "$Infinity", x == "$-Infinity", x == "$NaN", x == "$-0":
			return x[1:]
		case x[1] == '$':
			return x[1:]
		case x[1] == 'D', x[1] == 'n':
			return x[2:]
		case x[1] == 'Q', x[1] == 'W':
			return x // maps and sets: nothing here reads them
		case x[1] == 'L', x[1] == '@':
			if r, ok := f.ref(x[2:], resolving); ok {
				return r
			}
			return x
		}
		if r, ok := f.ref(x[1:], resolving); ok {
			return r
		}
		return x
	}
	return v
}

// ref follows "<id>" or "<id>:<key>:<key>..." into the rows.
func (f *mkFlight) ref(reference string, resolving map[string]bool) (any, bool) {
	segs := strings.Split(reference, ":")
	id := segs[0]
	if len(segs) == 1 {
		if t, ok := f.texts[id]; ok {
			return t, true
		}
	}
	if resolving[id] {
		return nil, false // a cycle
	}
	v, ok := f.models[id]
	if !ok {
		return nil, false
	}
	guard := map[string]bool{id: true}
	for k := range resolving {
		guard[k] = true
	}
	for _, seg := range segs[1:] {
		if s, isStr := v.(string); isStr && strings.HasPrefix(s, "$") {
			v = f.resolve(s, guard)
		}
		if v, ok = mkStep(v, seg); !ok {
			return nil, false
		}
	}
	return f.resolve(v, guard), true
}

// mkStep indexes one level into a row. React elements travel as
// ["$", type, key, props] tuples, so those names map to indices.
func mkStep(v any, seg string) (any, bool) {
	switch x := v.(type) {
	case map[string]any:
		e, ok := x[seg]
		return e, ok
	case []any:
		if len(x) >= 4 && x[0] == "$" {
			switch seg {
			case "type":
				return x[1], true
			case "key":
				return x[2], true
			case "props":
				return x[3], true
			}
		}
		i, err := strconv.Atoi(seg)
		if err != nil || i < 0 || i >= len(x) {
			return nil, false
		}
		return x[i], true
	}
	return nil, false
}
