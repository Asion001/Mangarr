package sites

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// XCOMIC (xcomic.me, also served as xcomic.net, comik.to and yona.to) speaks
// GraphQL. A work ("title") gathers several uploads ("comics"), each in one
// language by one group, and chapters belong to an upload. So a result is
// one upload of a title, and its url pins both: "<title id>:<comic id>", as
// Keiyoushi stores it. A bare comic id (Keiyoushi's older urls) still works.
//
// It is one catalog per translation language, plus "all" and "other".
var xcomicLangs = []string{
	"all", "en", "fr", "es", "es-419", "pt", "pt-BR", "ja", "ko", "zh", "zh-Hant", "ru", "id",
	"ab", "af", "sq", "am", "ar", "hy", "az", "be", "bn", "bs", "bg", "my", "km",
	"ca", "ceb", "hr", "cs", "cv", "da", "nl", "et", "eo", "eu", "fo", "fil", "fi",
	"ka", "de", "el", "gn", "gu", "ht", "ha", "he", "hi", "hu", "is", "ig", "ga",
	"gl", "it", "jv", "kn", "kk", "ku", "ky", "la", "lo", "lv", "lt", "lb", "mk",
	"mg", "ms", "ml", "mt", "mi", "mr", "mo", "mn", "ne", "no", "ny", "ps", "fa",
	"pl", "ro", "rm", "sm", "sr", "sh", "ss", "st", "sn", "sd", "si", "sk", "sl",
	"so", "sw", "sv", "tg", "ta", "te", "th", "ti", "to", "tr", "tk", "uk", "ur",
	"uz", "vi", "yo", "zu", "other",
}

const (
	xcomicSite = "https://xcomic.me"
	// xcomicPageSize is what the extension asks for (the API allows 48).
	xcomicPageSize = 12
	// xcomicTitlesInFlight and xcomicProbesPerTitle shape the fan-out that
	// turns a page of titles into uploads.
	xcomicTitlesInFlight = 3
	xcomicProbesPerTitle = 5
)

func init() {
	sourcekit.RegisterLangs("XCOMIC", 1, xcomicLangs, func(d sourcekit.Deps, lang string) sourcekit.Site {
		return &xcomic{c: d.Client, base: xcomicSite, lang: lang, dedupe: true,
			probes: map[string]xcomicProbe{}, fresh: map[string]int64{}}
	})
}

type xcomic struct {
	c *sourcekit.Client
	// base is the site (tests point it at a recorded copy).
	base string
	// lang is the catalog's language as Keiyoushi writes it.
	lang string
	// dedupe asks the site for one upload of each chapter.
	dedupe bool

	// probes caches what an upload is (language, chapter count), and fresh
	// the title's last-chapter time they were read at: while that hasn't
	// moved, a title's uploads need no new look.
	mu     sync.Mutex
	probes map[string]xcomicProbe
	fresh  map[string]int64
}

func (x *xcomic) Info() sourcekit.Info {
	return sourcekit.Info{ID: sourcekit.KeiyoushiID("XCOMIC", x.lang, 1), Name: "XCOMIC", Lang: x.lang, BaseURL: x.base,
		SupportsBrowse: true, IconURL: x.base + "/favicon.ico"}
}

// Politeness: the extension sets no limit of its own, so stay gentle.
func (x *xcomic) Politeness() sourcekit.Politeness {
	return sourcekit.Politeness{RequestsPerMinute: 60, MaxConcurrent: 2}
}

func (x *xcomic) Options() []sourcekit.Option {
	return []sourcekit.Option{{Key: "dedupe", Title: "One upload per chapter", Type: "switch", Value: x.dedupe,
		Help: "Let the site drop duplicate chapters. It may hide other groups' uploads."}}
}

func (x *xcomic) SetOption(key string, value any) error {
	if key != "dedupe" {
		return fmt.Errorf("%w: no option %q", sourcekit.ErrUnsupported, key)
	}
	switch v := value.(type) {
	case bool:
		x.dedupe = v
	case string:
		x.dedupe = v == "true" || v == "1"
	default:
		return fmt.Errorf("%v is not a switch", value)
	}
	return nil
}

// siteLang is the site's code for the catalog's language ("" for all).
func (x *xcomic) siteLang() string {
	switch x.lang {
	case "all":
		return ""
	case "pt-BR":
		return "pt_br"
	case "es-419":
		return "es_419"
	case "zh-Hant":
		return "zh_hk"
	case "other":
		return "_t"
	}
	return x.lang
}

// ---- GraphQL ----------------------------------------------------------------

// gql runs one query and decodes its data.
func (x *xcomic) gql(ctx context.Context, query string, variables, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	req := sourcekit.Request{Method: "POST", URL: x.base + "/query/", Body: body, Headers: map[string]string{
		"Content-Type": "application/json", "Referer": x.base + "/", "Origin": x.base}}
	if err := x.c.JSON(ctx, req, &env); err != nil {
		return err
	}
	if len(env.Errors) > 0 {
		msgs := make([]string, 0, len(env.Errors))
		for _, e := range env.Errors {
			msgs = append(msgs, e.Message)
		}
		return fmt.Errorf("xcomic: %s", strings.Join(msgs, "; "))
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return fmt.Errorf("xcomic: the answer has no data")
	}
	return json.Unmarshal(env.Data, out)
}

const xcomicBrowseQuery = `query get_title_browse($select: Title_Browse_Select) {
    get_title_browse_items(select: $select) {
        id
        data {
            title
            native_title
            romanized_title
            original_language
            translated_languages
            type
            cover_local_url
            cover_url
            comic_ids
            chap_last_public_at
        }
    }
}`

const xcomicTitleQuery = `query get_title_titleNode($id: ID!) {
    get_title_titleNode(id: $id) {
        id
        data {
            title
            alt_titles
            authors
            artists
            year
            type
            status
            description
            cover_local_url
            cover_url
            urlPath
            chap_last_public_at
            is_merged
            merged_to
            comic_ids
            content_rating_id
            type_id
            demographic_ids
            genre_ids
            format_ids
        }
    }
}`

const xcomicComicQuery = `query get_comicNode($id: ID!) {
    get_comicNode(id: $id) {
        id
        data {
            id
            name
            subName
            altNames
            authors
            artists
            originalLanguage
            translatedLanguage
            originalStatus
            uploadStatus
            type
            demographics
            contentRating
            genres
            tags
            dbStatus
            isPublic
            chaps_normal
            summary {
                text
            }
            urlPath
            urlCover
        }
    }
}`

const xcomicProbeQuery = `query get_comicNode($id: ID!) {
    get_comicNode(id: $id) {
        id
        data {
            name
            subName
            dbStatus
            isPublic
            translatedLanguage
            chaps_normal
            urlPath
            urlCover
        }
    }
}`

// xcomicChapterFields are the chapter fields both chapter lists answer with.
const xcomicChapterFields = `{
        paging {
            next
            total
        }
        items {
            id
            data {
                id
                serial
                dname
                title
                urlPath
                dateCreate
                datePublic
                dateModify
                chaNum
                srcName
                profileNodes {
                    data {
                        name
                    }
                }
            }
        }
    }`

const xcomicChaptersQuery = `query get_comic_chapterList_fullList($select: Select_Comic_ChapterList) {
    get_comic_chapterList_fullList(select: $select) ` + xcomicChapterFields + `
}`

const xcomicUniqChaptersQuery = `query get_comic_chapterList_uniqList($select: Select_Comic_ChapterList_UniqList) {
    get_comic_chapterList_uniqList(select: $select) ` + xcomicChapterFields + `
}`

const xcomicPagesQuery = `query($id: ID!) {
    get_chapterNode(id: $id) {
        id
        data {
            imageUrls
        }
    }
}`

// ---- the API's shapes (only the fields we use) ------------------------------

type xcomicBrowseNode struct {
	ID   string `json:"id"`
	Data *struct {
		Title          string   `json:"title"`
		CoverLocalURL  string   `json:"cover_local_url"`
		CoverURL       string   `json:"cover_url"`
		ComicIDs       []string `json:"comic_ids"`
		ChapLastPublic int64    `json:"chap_last_public_at"`
	} `json:"data"`
}

// xcomicProbe is the little the browse fan-out needs of an upload.
type xcomicProbe struct {
	Name               string `json:"name"`
	SubName            string `json:"subName"`
	DBStatus           string `json:"dbStatus"`
	IsPublic           *bool  `json:"isPublic"`
	TranslatedLanguage string `json:"translatedLanguage"`
	ChapsNormal        int    `json:"chaps_normal"`
	URLPath            string `json:"urlPath"`
	URLCover           string `json:"urlCover"`
}

// live: the upload is public and not taken down.
func (p xcomicProbe) live() bool {
	return (p.IsPublic == nil || *p.IsPublic) && (p.DBStatus == "" || p.DBStatus == "normal")
}

type xcomicTitle struct {
	Title          string   `json:"title"`
	AltTitles      []string `json:"alt_titles"`
	Authors        []string `json:"authors"`
	Artists        []string `json:"artists"`
	Type           string   `json:"type"`
	Description    string   `json:"description"`
	CoverLocalURL  string   `json:"cover_local_url"`
	CoverURL       string   `json:"cover_url"`
	ChapLastPublic int64    `json:"chap_last_public_at"`
	IsMerged       bool     `json:"is_merged"`
	MergedTo       string   `json:"merged_to"`
	ComicIDs       []string `json:"comic_ids"`
	ContentRating  string   `json:"content_rating_id"`
	Demographics   []string `json:"demographic_ids"`
	Genres         []string `json:"genre_ids"`
	Formats        []string `json:"format_ids"`
}

type xcomicComic struct {
	xcomicProbe
	Authors        []string `json:"authors"`
	Artists        []string `json:"artists"`
	OriginalStatus string   `json:"originalStatus"`
	UploadStatus   string   `json:"uploadStatus"`
	Type           string   `json:"type"`
	Demographics   []string `json:"demographics"`
	ContentRating  string   `json:"contentRating"`
	Genres         []string `json:"genres"`
	Summary        *struct {
		Text string `json:"text"`
	} `json:"summary"`
}

type xcomicChapter struct {
	ID           string   `json:"id"`
	Serial       *float64 `json:"serial"`
	DName        string   `json:"dname"`
	Title        string   `json:"title"`
	URLPath      string   `json:"urlPath"`
	DateCreate   int64    `json:"dateCreate"`
	DatePublic   int64    `json:"datePublic"`
	DateModify   int64    `json:"dateModify"`
	ChaNum       *float64 `json:"chaNum"`
	SrcName      string   `json:"srcName"`
	ProfileNodes []*struct {
		Data *struct {
			Name string `json:"name"`
		} `json:"data"`
	} `json:"profileNodes"`
}

// ---- lists ------------------------------------------------------------------

func (x *xcomic) Search(ctx context.Context, query string, page int) (sourcekit.Results, error) {
	return x.browse(ctx, page, strings.TrimSpace(query), "field_score")
}

func (x *xcomic) Popular(ctx context.Context, page int) (sourcekit.Results, error) {
	return x.browse(ctx, page, "", "field_score")
}

func (x *xcomic) Latest(ctx context.Context, page int) (sourcekit.Results, error) {
	return x.browse(ctx, page, "", "field_update")
}

// xcomicBrowseSelect is the browse query's input; fields left at their
// default are not sent, as the extension doesn't send them.
type xcomicBrowseSelect struct {
	Word      string   `json:"word,omitempty"`
	Page      int      `json:"page"`
	Size      int      `json:"size"`
	Init      int      `json:"init"`
	SortBy    string   `json:"sortby,omitempty"`
	Where     string   `json:"where"`
	IncTLangs []string `json:"incTLangs,omitempty"`
}

func (x *xcomic) browse(ctx context.Context, page int, query, sortBy string) (sourcekit.Results, error) {
	if page < 1 {
		page = 1
	}
	sel := xcomicBrowseSelect{Word: query, Page: page, Size: xcomicPageSize, Init: (page - 1) * xcomicPageSize,
		SortBy: sortBy, Where: "browse"}
	if l := x.siteLang(); l != "" {
		sel.IncTLangs = []string{l}
	}
	var out struct {
		Items []xcomicBrowseNode `json:"get_title_browse_items"`
	}
	if err := x.gql(ctx, xcomicBrowseQuery, map[string]any{"select": sel}, &out); err != nil {
		return sourcekit.Results{}, err
	}
	// each title becomes a row per live upload in the catalog's language,
	// a few titles at a time
	rows := make([][]sourcekit.Manga, len(out.Items))
	for start := 0; start < len(out.Items); start += xcomicTitlesInFlight {
		var wg sync.WaitGroup
		for i := start; i < min(start+xcomicTitlesInFlight, len(out.Items)); i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				rows[i] = x.uploads(ctx, out.Items[i], false)
			}(i)
		}
		wg.Wait()
	}
	var res sourcekit.Results
	for _, r := range rows {
		for _, m := range r {
			if !res.Has(m.URL) {
				res.Mangas = append(res.Mangas, m)
			}
		}
	}
	if len(res.Mangas) == 0 && ctx.Err() != nil {
		return res, ctx.Err()
	}
	res.HasNext = len(out.Items) >= xcomicPageSize
	return res, nil
}

// uploads are a title's live uploads in the catalog's language, the one
// with the most chapters first.
func (x *xcomic) uploads(ctx context.Context, t xcomicBrowseNode, force bool) []sourcekit.Manga {
	if t.ID == "" || t.Data == nil {
		return nil
	}
	var ids []string
	for _, id := range t.Data.ComicIDs {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	probes := x.probeAll(ctx, t.ID, t.Data.ChapLastPublic, ids, force)
	type row struct {
		id string
		p  xcomicProbe
	}
	var rows []row
	want := x.siteLang()
	for _, id := range ids {
		if p, ok := probes[id]; ok && p.live() && (want == "" || p.TranslatedLanguage == want) {
			rows = append(rows, row{id, p})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].p.ChapsNormal > rows[j].p.ChapsNormal })
	out := make([]sourcekit.Manga, 0, len(rows))
	for _, r := range rows {
		title := strings.TrimSpace(t.Data.Title)
		if title == "" {
			title = t.ID
		}
		if s := strings.TrimSpace(r.p.SubName); s != "" {
			title += " · " + html.UnescapeString(s)
		}
		if want == "" && r.p.TranslatedLanguage != "" {
			title += " [" + strings.ToUpper(r.p.TranslatedLanguage) + "]"
		}
		cover := t.Data.CoverLocalURL
		if cover == "" {
			cover = t.Data.CoverURL
		}
		if cover == "" {
			cover = r.p.URLCover
		}
		m := sourcekit.Manga{URL: t.ID + ":" + r.id, ID: t.ID + ":" + r.id, Title: title, CoverURL: x.abs(cover)}
		if r.p.ChapsNormal > 0 {
			n := r.p.ChapsNormal
			m.Chapters = &n
		}
		out = append(out, m)
	}
	return out
}

// probeAll looks at each of a title's uploads, reusing what it saw last time
// while the title has no newer chapter. Uploads it can't read are left out.
func (x *xcomic) probeAll(ctx context.Context, titleID string, lastPublic int64, ids []string, force bool) map[string]xcomicProbe {
	out := make(map[string]xcomicProbe, len(ids))
	x.mu.Lock()
	unchanged := !force && lastPublic > 0 && lastPublic <= x.fresh[titleID]
	var missing []string
	for _, id := range ids {
		if p, ok := x.probes[id]; ok && unchanged {
			out[id] = p
		} else {
			missing = append(missing, id)
		}
	}
	x.mu.Unlock()
	if len(missing) == 0 {
		return out
	}
	var mu sync.Mutex
	for start := 0; start < len(missing); start += xcomicProbesPerTitle {
		var wg sync.WaitGroup
		for _, id := range missing[start:min(start+xcomicProbesPerTitle, len(missing))] {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				var res struct {
					Node *struct {
						Data *xcomicProbe `json:"data"`
					} `json:"get_comicNode"`
				}
				if err := x.gql(ctx, xcomicProbeQuery, map[string]string{"id": id}, &res); err != nil || res.Node == nil || res.Node.Data == nil {
					return
				}
				mu.Lock()
				out[id] = *res.Node.Data
				mu.Unlock()
			}(id)
		}
		wg.Wait()
	}
	x.mu.Lock()
	if len(x.probes) > 5000 {
		x.probes, x.fresh = map[string]xcomicProbe{}, map[string]int64{} // bounded: titles come and go
	}
	for id, p := range out {
		x.probes[id] = p
	}
	if lastPublic > 0 {
		x.fresh[titleID] = lastPublic
	}
	x.mu.Unlock()
	return out
}

// abs makes the site's relative image paths absolute.
func (x *xcomic) abs(p string) string {
	switch {
	case p == "":
		return ""
	case strings.HasPrefix(p, "http"):
		return p
	}
	return x.base + p
}

// ---- one manga --------------------------------------------------------------

// xcomicSplit reads "<title id>:<comic id>" (a bare id is an older url: a
// title's, or an upload's).
func xcomicSplit(raw string) (title, comic string) {
	raw = strings.TrimSpace(raw)
	if i := strings.IndexByte(raw, ':'); i >= 0 {
		return raw[:i], raw[i+1:]
	}
	return raw, ""
}

func (x *xcomic) title(ctx context.Context, id string) (*xcomicTitle, error) {
	var out struct {
		Node *struct {
			Data *xcomicTitle `json:"data"`
		} `json:"get_title_titleNode"`
	}
	if err := x.gql(ctx, xcomicTitleQuery, map[string]string{"id": id}, &out); err != nil {
		return nil, err
	}
	if out.Node == nil || out.Node.Data == nil {
		return nil, nil
	}
	return out.Node.Data, nil
}

func (x *xcomic) comic(ctx context.Context, id string) (*xcomicComic, error) {
	var out struct {
		Node *struct {
			Data *xcomicComic `json:"data"`
		} `json:"get_comicNode"`
	}
	if err := x.gql(ctx, xcomicComicQuery, map[string]string{"id": id}, &out); err != nil {
		return nil, err
	}
	if out.Node == nil || out.Node.Data == nil {
		return nil, fmt.Errorf("%w: upload %s", sourcekit.ErrNotFound, id)
	}
	return out.Node.Data, nil
}

// pick chooses a title's upload for the catalog's language: the one with
// the most chapters (for "all", any live one will do).
func (x *xcomic) pick(ctx context.Context, titleID string, t *xcomicTitle) (string, bool) {
	var ids []string
	for _, id := range t.ComicIDs {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	probes := x.probeAll(ctx, titleID, t.ChapLastPublic, ids, false)
	want := x.siteLang()
	best, bestN, first := "", -1, ""
	for _, id := range ids {
		p, ok := probes[id]
		if !ok || !p.live() {
			continue
		}
		if first == "" {
			first = id
		}
		if (want == "" || p.TranslatedLanguage == want) && p.ChapsNormal > bestN {
			best, bestN = id, p.ChapsNormal
		}
	}
	if best == "" && want == "" {
		best = first
	}
	return best, best != ""
}

// resolve finds the upload a manga url means, and the title it belongs to
// (nil for an upload's bare id).
func (x *xcomic) resolve(ctx context.Context, raw string) (titleID string, t *xcomicTitle, comicID string, err error) {
	titleID, comicID = xcomicSplit(raw)
	if titleID == "" {
		return "", nil, "", fmt.Errorf("%q is not a manga url", raw)
	}
	t, err = x.title(ctx, titleID)
	if err != nil {
		return "", nil, "", err
	}
	if t == nil {
		if comicID != "" {
			return "", nil, "", fmt.Errorf("%w: title %s", sourcekit.ErrNotFound, titleID)
		}
		return "", nil, titleID, nil // an upload's id, from before titles
	}
	if t.IsMerged && t.MergedTo != "" && t.MergedTo != titleID {
		if merged, err := x.title(ctx, t.MergedTo); err == nil && merged != nil {
			t = merged
		}
	}
	if comicID == "" {
		var ok bool
		if comicID, ok = x.pick(ctx, titleID, t); !ok {
			return "", nil, "", fmt.Errorf("%w: no %s upload of title %s", sourcekit.ErrNotFound, x.lang, titleID)
		}
	}
	return titleID, t, comicID, nil
}

func (x *xcomic) Details(ctx context.Context, ref sourcekit.Ref) (sourcekit.Details, error) {
	titleID, t, comicID, err := x.resolve(ctx, ref.URL)
	if err != nil {
		return sourcekit.Details{}, err
	}
	c, err := x.comic(ctx, comicID)
	if err != nil {
		return sourcekit.Details{}, err
	}
	u := strings.TrimSpace(ref.URL) // kept as stored, pinned or not
	d := sourcekit.Details{Manga: sourcekit.Manga{URL: u, ID: u, Title: strings.TrimSpace(c.Name), CoverURL: x.abs(c.URLCover)},
		Author: strings.Join(c.Authors, ", "), Artist: strings.Join(c.Artists, ", "), Status: xcomicStatus(c.OriginalStatus, c.UploadStatus)}
	if c.Summary != nil {
		d.Description = strings.TrimSpace(c.Summary.Text)
	}
	genres := []string{}
	addGenre := func(g string) {
		if g = xcomicTagCase(g); g != "" && !xcomicContains(genres, g) {
			genres = append(genres, g)
		}
	}
	addGenre(c.Type)
	for _, g := range c.Demographics {
		addGenre(g)
	}
	addGenre(c.ContentRating)
	for _, g := range c.Genres {
		addGenre(g)
	}
	if t != nil {
		if title := strings.TrimSpace(t.Title); title != "" {
			d.Title = title
		}
		if len(t.Authors) > 0 {
			d.Author = strings.Join(t.Authors, ", ")
		}
		if len(t.Artists) > 0 {
			d.Artist = strings.Join(t.Artists, ", ")
		}
		if cover := xcomicFirst(t.CoverLocalURL, t.CoverURL); cover != "" {
			d.CoverURL = x.abs(cover)
		}
		if desc := strings.TrimSpace(t.Description); desc != "" {
			d.Description = desc
		}
		addGenre(t.Type)
		for _, g := range t.Demographics {
			addGenre(g)
		}
		addGenre(t.ContentRating)
		for _, g := range append(t.Genres, t.Formats...) {
			addGenre(g)
		}
	}
	if s := strings.TrimSpace(c.SubName); s != "" {
		d.Title += " · " + html.UnescapeString(s)
	}
	d.Genres = genres
	switch {
	case c.URLPath != "":
		d.WebURL = x.base + c.URLPath
	case t != nil:
		d.WebURL = x.base + "/title/" + titleID
	default:
		d.WebURL = x.base + "/source/" + comicID
	}
	if c.ChapsNormal > 0 {
		n := c.ChapsNormal
		d.Chapters = &n
	}
	return d, nil
}

// xcomicStatus reads an upload's status: the original work's, else the
// upload's. A finished work still being translated counts as ongoing here.
func xcomicStatus(original, upload string) string {
	s := original
	if s == "" {
		s = upload
	}
	switch {
	case s == "", strings.Contains(s, "pending"):
		return sourcekit.StatusUnknown
	case strings.Contains(s, "ongoing"):
		return sourcekit.StatusOngoing
	case strings.Contains(s, "cancelled"):
		return sourcekit.StatusCancelled
	case strings.Contains(s, "hiatus"):
		return sourcekit.StatusHiatus
	case strings.Contains(s, "completed"):
		if strings.Contains(upload, "ongoing") {
			return sourcekit.StatusOngoing
		}
		return sourcekit.StatusCompleted
	}
	return sourcekit.StatusUnknown
}

// xcomicTagCase writes a slug the way the extension shows it ("slice_of_life"
// becomes "Slice Of Life").
func xcomicTagCase(s string) string {
	words := strings.Fields(strings.ReplaceAll(s, "_", " "))
	for i, w := range words {
		words[i] = xcomicUpperFirst(strings.ToLower(w))
	}
	return strings.Join(words, " ")
}

// xcomicUpperFirst capitalises a word's first letter.
func xcomicUpperFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToTitle(r)) + s[size:]
}

func xcomicContains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func xcomicFirst(xs ...string) string {
	for _, s := range xs {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

func (x *xcomic) Chapters(ctx context.Context, ref sourcekit.Ref) ([]sourcekit.Chapter, error) {
	comicID := ""
	if _, pinned := xcomicSplit(ref.URL); pinned != "" {
		comicID = pinned
	} else {
		_, _, id, err := x.resolve(ctx, ref.URL)
		if err != nil {
			return nil, err
		}
		comicID = id
	}
	query, field, size := xcomicChaptersQuery, "get_comic_chapterList_fullList", 100
	if x.dedupe {
		query, field, size = xcomicUniqChaptersQuery, "get_comic_chapterList_uniqList", 1000
	}
	var out []sourcekit.Chapter
	for page, pages := 1, 1; page <= pages; page++ {
		var res map[string]struct {
			Paging struct {
				Next  int `json:"next"`
				Total int `json:"total"`
			} `json:"paging"`
			Items []struct {
				Data xcomicChapter `json:"data"`
			} `json:"items"`
		}
		vars := map[string]any{"select": map[string]any{"comic_id": comicID, "page": page, "size": size}}
		if err := x.gql(ctx, query, vars, &res); err != nil {
			return nil, err
		}
		list := res[field]
		for _, it := range list.Items {
			out = append(out, x.chapter(it.Data))
		}
		if page == 1 && list.Paging.Total > size && list.Paging.Next != 0 {
			pages = (list.Paging.Total + size - 1) / size
		}
	}
	return out, nil
}

// chapter names a chapter the way the extension does ("Chapter 12: Title").
func (x *xcomic) chapter(c xcomicChapter) sourcekit.Chapter {
	num := c.ChaNum
	if num == nil {
		num = c.Serial
	}
	var name strings.Builder
	ch := sourcekit.Chapter{URL: c.ID, ID: c.ID, Number: -1}
	if num != nil {
		ch.Number = *num
		if n := strconv.FormatFloat(*num, 'f', -1, 32); !strings.Contains(c.DName, n) {
			name.WriteString("Chapter " + n)
		}
	}
	for _, part := range []string{c.DName, c.Title} {
		if part == "" {
			continue
		}
		if name.Len() > 0 {
			name.WriteString(": ")
		}
		name.WriteString(part)
	}
	ch.Name = name.String()
	for _, ms := range []int64{c.DateModify, c.DateCreate, c.DatePublic} {
		if ms > 0 {
			t := time.UnixMilli(ms).UTC()
			ch.UploadedAt = &t
			break
		}
	}
	if s := c.SrcName; s != "" {
		ch.Scanlator = xcomicUpperFirst(s)
	} else {
		var names []string
		for _, p := range c.ProfileNodes {
			if p != nil && p.Data != nil && p.Data.Name != "" {
				names = append(names, p.Data.Name)
			}
		}
		ch.Scanlator = strings.Join(names, ", ")
	}
	if c.URLPath != "" {
		ch.WebURL = x.base + c.URLPath
	} else {
		ch.WebURL = x.base + "/chapter/" + c.ID
	}
	return ch
}

func (x *xcomic) Pages(ctx context.Context, ch sourcekit.PageRef) ([]sourcekit.PageImage, error) {
	id := strings.TrimSpace(ch.URL)
	if id == "" {
		id = ch.ID
	}
	if id == "" || strings.Contains(id, "/") {
		return nil, fmt.Errorf("%q is not a chapter url", ch.URL)
	}
	var out struct {
		Node *struct {
			Data struct {
				ImageURLs []string `json:"imageUrls"`
			} `json:"data"`
		} `json:"get_chapterNode"`
	}
	if err := x.gql(ctx, xcomicPagesQuery, map[string]string{"id": id}, &out); err != nil {
		return nil, err
	}
	if out.Node == nil {
		return nil, fmt.Errorf("%w: chapter %s", sourcekit.ErrNotFound, id)
	}
	var pages []sourcekit.PageImage
	for _, u := range out.Node.Data.ImageURLs {
		if u = strings.TrimSpace(u); u == "" {
			continue
		}
		pages = append(pages, sourcekit.PageImage{Index: len(pages), URL: x.abs(u), Headers: map[string]string{"Referer": x.base + "/"}})
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: the chapter has no pages", sourcekit.ErrNotFound)
	}
	return pages, nil
}
