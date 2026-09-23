package sites

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// MangaDot (mangadot.net) has a JSON API for search, chapters and pages, and
// serves its browse and manga pages as React Router data (".data" routes),
// which are decoded here the way the site's own client does.
//
// It is one catalog per chapter language. The languages the site started
// with carry the ids Keiyoushi pinned for them; the rest get the usual
// derived id, so every one links up with a Mihon backup.
var mdotPinnedIDs = map[string]string{
	"ar": "5133570518916566066", "bn": "4728703871864086205", "bg": "4621039982977056475",
	"my": "8689086897953658974", "zh": "4593442970144109426", "zh-Hant": "2076066796458496830",
	"cs": "182506561627032263", "da": "522919629093846860", "nl": "5339181991315919474",
	"en": "5900936305360403385", "tl": "5536176722691621839", "fi": "7568879765052968178",
	"fr": "6544312035114371248", "ka": "7781185259229560796", "de": "1739134904773959471",
	"el": "6280808899001059050", "he": "7524478288761759786", "hi": "4686432307246610016",
	"hu": "2744567066632059507", "id": "8591108444263884327", "it": "8788147393700258423",
	"ja": "2305771977147956314", "ko": "8733946525904795862", "la": "5980076819966447323",
	"lt": "3456620422576095825", "ms": "2379671138411944871", "mn": "9192319104809604483",
	"no": "2111970709663576933", "fa": "6840185760082019759", "pl": "516446519459282312",
	"pt": "1374245104599191336", "pt-BR": "6883842335519142390", "ro": "7223291528565862680",
	"ru": "8911989140118399619", "es": "1356109540530417190", "es-419": "5046796980408019790",
	"sv": "4958143963089747877", "th": "8348427309728988846", "tr": "5964430973280552505",
	"uk": "3578850460057110410", "vi": "3741155905873931805",
}

// mdotNewLangs are the languages added later, with derived ids.
var mdotNewLangs = []string{
	"zu", "zh-tw", "yo", "uz", "ur", "tk", "to", "ti", "te", "ta", "tg", "ss", "sw",
	"so", "sl", "sk", "si", "sd", "sn", "st", "sh", "sr", "sm", "rm",
	"ps", "ny", "ne", "mo", "mr", "mi", "mt", "ml", "mg", "mk", "lb", "lv", "lo",
	"ky", "ku", "kk", "kn", "jv", "ga", "ig", "is", "ha", "ht", "gu", "gn", "gl",
	"fo", "et", "eo", "hr", "cv", "ceb", "ca", "km", "bs", "be", "eu", "az", "hy",
	"am", "sq", "af", "ab",
}

const (
	mdotSite     = "https://mangadot.net"
	mdotPageSize = 56
)

func init() {
	for lang := range mdotPinnedIDs {
		sourcekit.Register(mdotID(lang), mdotBuilder(lang))
	}
	for _, lang := range mdotNewLangs {
		sourcekit.Register(mdotID(lang), mdotBuilder(lang))
	}
}

func mdotBuilder(lang string) sourcekit.Builder {
	return func(d sourcekit.Deps) sourcekit.Site {
		return &mangadot{c: d.Client, base: mdotSite, lang: lang, adult: "none", rating: "suggestive"}
	}
}

// mdotID is the catalog id for one of Keiyoushi's language codes.
func mdotID(lang string) string {
	if id, ok := mdotPinnedIDs[lang]; ok {
		return id
	}
	return sourcekit.KeiyoushiID("MangaDot", lang, 1)
}

type mangadot struct {
	c *sourcekit.Client
	// base is the site (tests point it at a recorded copy).
	base string
	// lang is the catalog's language as Keiyoushi writes it.
	lang string
	// adult is what browsing shows: "none", "1" (18+ only) or "both".
	adult string
	// rating is the highest content rating search shows.
	rating string
}

func (m *mangadot) Info() sourcekit.Info {
	return sourcekit.Info{ID: mdotID(m.lang), Name: "MangaDot", Lang: m.lang, BaseURL: m.base, SupportsBrowse: true,
		IconURL: m.base + "/favicon.ico"}
}

// Politeness: the extension sets no limit of its own, so stay gentle.
func (m *mangadot) Politeness() sourcekit.Politeness {
	return sourcekit.Politeness{RequestsPerMinute: 60, MaxConcurrent: 2}
}

// mdotRatings are the site's content ratings, mildest first.
var mdotRatings = []string{"safe", "suggestive", "erotica", "pornographic"}

func (m *mangadot) Options() []sourcekit.Option {
	return []sourcekit.Option{
		{Key: "adult", Title: "18+ titles in popular and latest", Type: "select", Value: m.adult, Choices: []sourcekit.Choice{
			{Value: "none", Label: "No 18+"}, {Value: "1", Label: "18+ only"}, {Value: "both", Label: "Both"}}},
		{Key: "rating", Title: "Highest content rating in search", Type: "select", Value: m.rating, Choices: []sourcekit.Choice{
			{Value: "safe", Label: "Safe"}, {Value: "suggestive", Label: "Suggestive"},
			{Value: "erotica", Label: "Erotica"}, {Value: "pornographic", Label: "Pornographic"}}},
	}
}

func (m *mangadot) SetOption(key string, value any) error {
	v, _ := value.(string)
	switch key {
	case "adult":
		if v != "none" && v != "1" && v != "both" {
			return fmt.Errorf("%v is not an 18+ mode", value)
		}
		m.adult = v
	case "rating":
		if mdotRatingIndex(v) < 0 {
			return fmt.Errorf("%v is not a content rating", value)
		}
		m.rating = v
	default:
		return fmt.Errorf("%w: no option %q", sourcekit.ErrUnsupported, key)
	}
	return nil
}

func mdotRatingIndex(r string) int {
	for i, x := range mdotRatings {
		if x == r {
			return i
		}
	}
	return -1
}

// queryLang is the site's code for the catalog's language.
func (m *mangadot) queryLang() string {
	switch m.lang {
	case "pt-BR":
		return "pt-br"
	case "es-419":
		return "es-la"
	case "zh-Hant":
		return "zh-hk"
	}
	return m.lang
}

func (m *mangadot) headers(referer string) map[string]string {
	if referer == "" {
		referer = m.base + "/"
	}
	return map[string]string{"Referer": referer, "Origin": m.base}
}

// ---- the site's shapes (only the fields we use) -----------------------------

// mdotList is a page of results. The site names its fields differently from
// one endpoint to the next, so every spelling it uses is accepted.
type mdotList struct {
	MangaList  []mdotBrowse `json:"mangaList"`
	Results    []mdotBrowse `json:"results"`
	MangaList2 []mdotBrowse `json:"manga_list"`
	Pagination *struct {
		TotalPages  *int   `json:"total_pages"`
		TotalPages2 *int   `json:"totalPages"`
		LastPage    *int   `json:"last_page"`
		LastPage2   *int   `json:"lastPage"`
		CurrentPage *int   `json:"current_page"`
		Current2    *int   `json:"currentPage"`
		Page        *int   `json:"page"`
		NextCursor  string `json:"next_cursor"`
		NextCursor2 string `json:"nextCursor"`
		Cursor      string `json:"cursor"`
	} `json:"pagination"`
}

func (l mdotList) items() []mdotBrowse {
	for _, x := range [][]mdotBrowse{l.MangaList, l.Results, l.MangaList2} {
		if len(x) > 0 {
			return x
		}
	}
	return nil
}

func (l mdotList) hasNext() bool {
	p := l.Pagination
	if p == nil {
		return false
	}
	first := func(xs ...*int) *int {
		for _, x := range xs {
			if x != nil {
				return x
			}
		}
		return nil
	}
	total := first(p.TotalPages, p.TotalPages2, p.LastPage, p.LastPage2)
	current := first(p.CurrentPage, p.Current2, p.Page)
	if total != nil && current != nil {
		return *current < *total
	}
	return p.NextCursor != "" || p.NextCursor2 != "" || p.Cursor != ""
}

type mdotBrowse struct {
	MangaID int    `json:"manga_id"`
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Photo   string `json:"photo"`
}

type mdotManga struct {
	ID          int      `json:"id"`
	Title       string   `json:"title"`
	Genres      []string `json:"genres"`
	Description string   `json:"description"`
	Photo       string   `json:"photo"`
	Hiatus      string   `json:"hiatus"`
	Status      string   `json:"status"`
	Origin      string   `json:"country_of_origin"`
	Tags        []struct {
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
	} `json:"tags"`
	// Authors and Artists are JSON lists sent as strings.
	Authors json.RawMessage `json:"authors"`
	Artists json.RawMessage `json:"artists"`
}

type mdotChapter struct {
	ID        int      `json:"id"`
	Number    *float64 `json:"chapter_number"`
	Title     string   `json:"chapter_title"`
	Language  *string  `json:"language"`
	Group     string   `json:"group_name"`
	Scanlator string   `json:"scanlator_name"`
	Date      string   `json:"date_added"`
	Source    *string  `json:"source"`
}

// mdotChapterRef is how Keiyoushi stores a chapter's url, kept as it is so
// read chapters in a Mihon backup match up.
type mdotChapterRef struct {
	ID       string `json:"id"`
	Source   string `json:"source"`
	IsVolume bool   `json:"isVolume"`
}

// ---- lists ------------------------------------------------------------------

func (m *mangadot) Search(ctx context.Context, query string, page int) (sourcekit.Results, error) {
	if page < 1 {
		page = 1
	}
	q := url.Values{}
	query = strings.TrimSpace(query)
	if query != "" {
		q.Set("search", query)
	} else {
		q.Set("sortBy", "latest")
	}
	q.Set("page", strconv.Itoa(page))
	q.Set("limit", strconv.Itoa(mdotPageSize))
	q.Set("sortOrder", "desc")
	if i := mdotRatingIndex(m.rating); i >= 0 && i < len(mdotRatings)-1 {
		var excluded []string
		for _, r := range mdotRatings[i+1:] {
			excluded = append(excluded, "-"+r)
		}
		q.Set("content_rating", strings.Join(excluded, ","))
	}
	var out mdotList
	if err := m.c.JSON(ctx, sourcekit.Request{URL: m.base + "/api/search", Query: q, Headers: m.headers("")}, &out); err != nil {
		return sourcekit.Results{}, err
	}
	res := m.results(out.items())
	res.HasNext = out.hasNext() && len(res.Mangas) > 0
	return res, nil
}

func (m *mangadot) Popular(ctx context.Context, page int) (sourcekit.Results, error) {
	return m.viewAll(ctx, "most-tracked", page)
}

func (m *mangadot) Latest(ctx context.Context, page int) (sourcekit.Results, error) {
	return m.viewAll(ctx, "latest-updates", page)
}

// viewAll reads one of the site's "view all" pages.
func (m *mangadot) viewAll(ctx context.Context, mode string, page int) (sourcekit.Results, error) {
	const route = "pages/ViewAllPage"
	q := url.Values{}
	switch m.adult {
	case "1", "both":
		q.Set("adult", m.adult)
	default:
		q.Set("adult", "0")
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	q.Set("_routes", route)
	var out struct {
		Data struct {
			Data mdotList `json:"data"`
		} `json:"data"`
	}
	if err := m.routeData(ctx, m.base+"/view-all/"+mode+".data", q, route, &out); err != nil {
		return sourcekit.Results{}, err
	}
	res := m.results(out.Data.Data.items())
	res.HasNext = out.Data.Data.hasNext()
	return res, nil
}

func (m *mangadot) results(items []mdotBrowse) sourcekit.Results {
	var res sourcekit.Results
	for _, x := range items {
		id := x.MangaID
		if id == 0 {
			id = x.ID
		}
		if id == 0 || res.Has(strconv.Itoa(id)) {
			continue
		}
		res.Mangas = append(res.Mangas, sourcekit.Manga{URL: strconv.Itoa(id), ID: strconv.Itoa(id),
			Title: strings.TrimSpace(x.Title), CoverURL: m.image(x.Photo)})
	}
	return res
}

// image makes the site's image paths absolute (anything else is dropped).
func (m *mangadot) image(p string) string {
	switch {
	case strings.HasPrefix(p, "/"):
		return m.base + p
	case strings.HasPrefix(p, "http"):
		return p
	}
	return ""
}

// ---- one manga --------------------------------------------------------------

func (m *mangadot) Details(ctx context.Context, ref sourcekit.Ref) (sourcekit.Details, error) {
	id := mdotMangaID(ref)
	if id == "" {
		return sourcekit.Details{}, fmt.Errorf("%q is not a manga url", ref.URL)
	}
	const route = "pages/MangaDetailPage"
	var out struct {
		Data struct {
			MangaData struct {
				Manga mdotManga `json:"manga"`
			} `json:"mangaData"`
		} `json:"data"`
	}
	if err := m.routeData(ctx, m.base+"/manga/"+id+".data", url.Values{"_routes": {route}}, route, &out); err != nil {
		return sourcekit.Details{}, notFound(err)
	}
	x := out.Data.MangaData.Manga
	if x.ID == 0 && x.Title == "" {
		return sourcekit.Details{}, fmt.Errorf("%w: manga %s", sourcekit.ErrNotFound, id)
	}
	d := sourcekit.Details{
		Manga:  sourcekit.Manga{URL: id, ID: id, Title: strings.TrimSpace(x.Title), CoverURL: m.image(x.Photo)},
		Author: strings.Join(mdotNames(x.Authors), ", "), Artist: strings.Join(mdotNames(x.Artists), ", "),
		Description: mdotDescription(x.Description), WebURL: m.base + "/manga/" + id,
	}
	switch x.Origin {
	case "JP":
		d.Genres = append(d.Genres, "Manga")
	case "KR":
		d.Genres = append(d.Genres, "Manhwa")
	case "CN":
		d.Genres = append(d.Genres, "Manhua")
	case "EN":
		d.Genres = append(d.Genres, "OEL")
	}
	oneShot := false
	for _, g := range x.Genres {
		d.Genres = append(d.Genres, strings.TrimSpace(g))
		oneShot = oneShot || g == "One Shot"
	}
	var tags []string
	for _, cat := range x.Tags {
		for _, t := range cat.Tags {
			if n := strings.TrimSpace(t.Name); n != "" {
				tags = append(tags, n)
			}
		}
	}
	sort.SliceStable(tags, func(i, j int) bool { return strings.ToLower(tags[i]) < strings.ToLower(tags[j]) })
	d.Genres = append(d.Genres, tags...)
	switch {
	case oneShot:
		d.Status = sourcekit.StatusCompleted
	case x.Hiatus == "Yes":
		d.Status = sourcekit.StatusHiatus
	case strings.EqualFold(x.Status, "ongoing"):
		d.Status = sourcekit.StatusOngoing
	case strings.EqualFold(x.Status, "completed"):
		d.Status = sourcekit.StatusCompleted
	default:
		d.Status = sourcekit.StatusUnknown
	}
	return d, nil
}

// mdotNames reads a list of names the site sends as a JSON string (or, to be
// safe, as a plain list).
func mdotNames(raw json.RawMessage) []string {
	var names []string
	var s string
	if json.Unmarshal(raw, &s) == nil {
		_ = json.Unmarshal([]byte(s), &names)
	} else {
		_ = json.Unmarshal(raw, &names)
	}
	return names
}

// mdotDescription tidies the site's description the way its extension does.
func mdotDescription(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}

func (m *mangadot) Chapters(ctx context.Context, ref sourcekit.Ref) ([]sourcekit.Chapter, error) {
	id := mdotMangaID(ref)
	if id == "" {
		return nil, fmt.Errorf("%q is not a manga url", ref.URL)
	}
	var list []mdotChapter
	req := sourcekit.Request{URL: m.base + "/api/manga/" + id + "/chapters/list",
		Query: url.Values{"lang": {m.queryLang()}}, Headers: m.headers(m.base + "/manga/" + id)}
	if err := m.c.JSON(ctx, req, &list); err != nil {
		return nil, notFound(err)
	}
	out := make([]sourcekit.Chapter, 0, len(list))
	// the site lists oldest first
	for i := len(list) - 1; i >= 0; i-- {
		c := list[i]
		if c.Language != nil && !strings.EqualFold(*c.Language, m.lang) && !strings.EqualFold(*c.Language, m.queryLang()) {
			continue
		}
		source := "user"
		if c.Source != nil {
			source = *c.Source
		}
		cref := mdotChapterRef{ID: strconv.Itoa(c.ID), Source: source}
		raw, _ := json.Marshal(cref)
		ch := sourcekit.Chapter{URL: string(raw), ID: cref.ID, Number: -1, WebURL: m.chapterPage(cref)}
		number := "0"
		if c.Number != nil {
			ch.Number = *c.Number
			number = strconv.FormatFloat(*c.Number, 'f', -1, 32)
		}
		name := strings.TrimSpace(c.Title)
		if !strings.Contains(name, number) {
			name = strings.TrimSuffix(strings.TrimSpace("Chapter "+number+": "+name), ":")
		}
		ch.Name = name
		if g := strings.TrimSpace(c.Group); g != "" {
			ch.Scanlator = g
		} else if s := strings.TrimSpace(c.Scanlator); s != "" {
			ch.Scanlator = s
		}
		if t, ok := mdotTime(c.Date); ok {
			ch.UploadedAt = &t
		}
		out = append(out, ch)
	}
	return out, nil
}

// mdotTime reads the site's dates ("2025-01-02 03:04:05", with or without an
// offset); a date without one is taken as UTC.
func mdotTime(s string) (time.Time, bool) {
	s = strings.Replace(strings.TrimSpace(s), " ", "T", 1)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05Z0700"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// chapterPage is the chapter's page at the site.
func (m *mangadot) chapterPage(c mdotChapterRef) string {
	if c.IsVolume {
		return m.base + "/volume/" + c.ID
	}
	u := m.base + "/chapter/" + c.ID
	if c.Source == "user" {
		u += "?source=user"
	}
	return u
}

func (m *mangadot) Pages(ctx context.Context, ch sourcekit.PageRef) ([]sourcekit.PageImage, error) {
	c, ok := mdotParseChapter(ch.URL)
	if !ok {
		return nil, fmt.Errorf("%q is not a chapter url", ch.URL)
	}
	segment := "chapters"
	if c.Source == "user" {
		segment = "uploads"
	}
	referer := m.chapterPage(c)
	var out struct {
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
	}
	req := sourcekit.Request{URL: m.base + "/api/" + segment + "/" + c.ID + "/images", Headers: m.headers(referer)}
	if err := m.c.JSON(ctx, req, &out); err != nil {
		return nil, notFound(err)
	}
	var pages []sourcekit.PageImage
	for _, img := range out.Images {
		u := m.image(img.URL)
		if u == "" {
			continue
		}
		pages = append(pages, sourcekit.PageImage{Index: len(pages), URL: u, Headers: map[string]string{"Referer": referer}})
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: the chapter has no pages", sourcekit.ErrNotFound)
	}
	return pages, nil
}

// mdotMangaID is the numeric id Keiyoushi stores as the url ("12345"), also
// read out of the site's own "/manga/12345" links.
func mdotMangaID(ref sourcekit.Ref) string {
	p := strings.Trim(sourcekit.Path(ref.URL), "/")
	p, _, _ = strings.Cut(strings.TrimPrefix(p, "manga/"), "/")
	if p == "" {
		p = ref.ID
	}
	if _, err := strconv.Atoi(p); err != nil {
		return ""
	}
	return p
}

// mdotParseChapter reads a chapter url: Keiyoushi's JSON, or a link to the
// chapter's page at the site.
func mdotParseChapter(raw string) (mdotChapterRef, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "{") {
		var c mdotChapterRef
		if err := json.Unmarshal([]byte(raw), &c); err != nil || c.ID == "" {
			return c, false
		}
		return c, true
	}
	u, err := url.Parse(raw)
	if err != nil {
		return mdotChapterRef{}, false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[1] == "" {
		return mdotChapterRef{}, false
	}
	switch parts[0] {
	case "chapter":
		src := u.Query().Get("source")
		if src == "" {
			src = "site"
		}
		return mdotChapterRef{ID: parts[1], Source: src}, true
	case "volume":
		return mdotChapterRef{ID: parts[1], Source: "user", IsVolume: true}, true
	}
	return mdotChapterRef{}, false
}

// ---- React Router data ------------------------------------------------------

// routeData fetches a ".data" route and decodes the part for one route.
func (m *mangadot) routeData(ctx context.Context, endpoint string, q url.Values, route string, out any) error {
	var flat []json.RawMessage
	if err := m.c.JSON(ctx, sourcekit.Request{URL: endpoint, Query: q, Headers: m.headers("")}, &flat); err != nil {
		return err
	}
	decoded, err := mdotDecodeFlat(flat)
	if err != nil {
		return fmt.Errorf("%s: %w", endpoint, err)
	}
	root, ok := decoded.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: the answer is not an object", endpoint)
	}
	part, ok := root[route]
	if !ok {
		return fmt.Errorf("%s: route %q is not in the answer", endpoint, route)
	}
	raw, err := json.Marshal(part)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// mdotDecodeFlat rebuilds the value React Router flattens into one array:
// the root is element 0, an array holds the indexes of its items, an object
// maps "_<index of the key>" to the index of the value, and a negative index
// is a missing value.
func mdotDecodeFlat(flat []json.RawMessage) (any, error) {
	if len(flat) == 0 {
		return nil, fmt.Errorf("empty data")
	}
	done := make([]bool, len(flat))
	cache := make([]any, len(flat))
	busy := make([]bool, len(flat))
	var resolve func(i int) (any, error)
	resolve = func(i int) (any, error) {
		if i < 0 || i >= len(flat) || busy[i] {
			return nil, nil
		}
		if done[i] {
			return cache[i], nil
		}
		busy[i] = true
		defer func() { busy[i] = false }()
		raw := flat[i]
		var v any
		switch trimmed := strings.TrimSpace(string(raw)); {
		case trimmed == "null":
		case strings.HasPrefix(trimmed, "["):
			var idx []int
			if err := json.Unmarshal(raw, &idx); err != nil {
				return nil, fmt.Errorf("element %d: %w", i, err)
			}
			arr := make([]any, len(idx))
			for k, j := range idx {
				x, err := resolve(j)
				if err != nil {
					return nil, err
				}
				arr[k] = x
			}
			v = arr
		case strings.HasPrefix(trimmed, "{"):
			var obj map[string]int
			if err := json.Unmarshal(raw, &obj); err != nil {
				return nil, fmt.Errorf("element %d: %w", i, err)
			}
			m := make(map[string]any, len(obj))
			for k, j := range obj {
				ki, err := strconv.Atoi(strings.TrimPrefix(k, "_"))
				if err != nil || ki < 0 || ki >= len(flat) {
					return nil, fmt.Errorf("element %d: bad key %q", i, k)
				}
				var key string
				if err := json.Unmarshal(flat[ki], &key); err != nil {
					return nil, fmt.Errorf("element %d: key %q: %w", i, k, err)
				}
				x, err := resolve(j)
				if err != nil {
					return nil, err
				}
				m[key] = x
			}
			v = m
		default:
			v = raw // a string, number or bool, kept as it came
		}
		cache[i], done[i] = v, true
		return v, nil
	}
	return resolve(0)
}
