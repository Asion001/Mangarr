package sites

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// MANGA Plus is Shueisha's own reader. Its API answers in protobuf, read here
// field by field (only the fields Keiyoushi's extension reads), and its page
// images arrive XOR-encrypted with a key the chapter's viewer hands out.
//
// It is one catalog per language, with the ids Mihon's "MANGA Plus by
// SHUEISHA" extension gives its sources. Manga and chapter urls are the ones
// that extension stores ("#/titles/<id>", "#/viewer/<id>"), so a library and
// its read chapters imported from a Mihon backup link up here.
const mpName = "MANGA Plus by SHUEISHA"

var mpLangs = []string{"en", "es", "fr", "id", "pt-BR", "ru", "th", "vi", "de"}

const (
	mpSite = "https://mangaplus.shueisha.co.jp"
	mpAPI  = "https://jumpg-webapi.tokyo-cdn.com/api"
)

func init() {
	sourcekit.RegisterLangs(mpName, 1, mpLangs, func(d sourcekit.Deps, lang string) sourcekit.Site {
		return &mangaplus{c: d.Client, site: mpSite, api: mpAPI, code: lang, session: mpSessionToken(),
			quality: "super_high", split: true}
	})
}

// mpLang is the API's name and number for one of Keiyoushi's language codes.
func mpLang(code string) (string, int) {
	switch code {
	case "es":
		return "esp", 1
	case "fr":
		return "fra", 2
	case "id":
		return "ind", 3
	case "pt-BR":
		return "ptb", 4
	case "ru":
		return "rus", 5
	case "th":
		return "tha", 6
	case "de":
		return "deu", 7
	case "vi":
		return "vie", 9
	}
	return "eng", 0
}

type mangaplus struct {
	c *sourcekit.Client
	// site and api are the addresses to talk to (tests point them at a
	// recorded copy).
	site, api string
	// code is the catalog's language as Keiyoushi writes it.
	code string
	// session is sent as SESSION-TOKEN with every API call, as the app does.
	session string
	// quality ("low", "high", "super_high"), split double pages, and name
	// chapters by their subtitle alone.
	quality      string
	split        bool
	subtitleOnly bool
}

// mpSessionToken is a random uuid, like the one the app makes per session.
func mpSessionToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func (m *mangaplus) Info() sourcekit.Info {
	return sourcekit.Info{ID: sourcekit.KeiyoushiID(mpName, m.code, 1), Name: "MANGA Plus", Lang: m.code, BaseURL: m.site,
		SupportsBrowse: true, IconURL: m.site + "/favicon.ico"}
}

// Politeness: the extension holds the API to one request a second.
func (m *mangaplus) Politeness() sourcekit.Politeness {
	return sourcekit.Politeness{RequestsPerMinute: 60, MaxConcurrent: 2}
}

func (m *mangaplus) Options() []sourcekit.Option {
	return []sourcekit.Option{
		{Key: "quality", Title: "Image quality", Type: "select", Value: m.quality, Choices: []sourcekit.Choice{
			{Value: "low", Label: "Low"}, {Value: "high", Label: "Medium"}, {Value: "super_high", Label: "High"}}},
		{Key: "split", Title: "Split double pages", Type: "switch", Value: m.split},
		{Key: "subtitle_only", Title: "Name chapters by their subtitle only", Type: "switch", Value: m.subtitleOnly,
			Help: "Applies the next time the chapter list is refreshed."},
	}
}

func (m *mangaplus) SetOption(key string, value any) error {
	switch key {
	case "quality":
		s, _ := value.(string)
		switch s {
		case "low", "high", "super_high":
			m.quality = s
		default:
			return fmt.Errorf("%v is not an image quality", value)
		}
	case "split":
		m.split = mpBool(value)
	case "subtitle_only":
		m.subtitleOnly = mpBool(value)
	default:
		return fmt.Errorf("%w: no option %q", sourcekit.ErrUnsupported, key)
	}
	return nil
}

func mpBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true" || x == "1"
	}
	return false
}

// ---- the API's shapes ---------------------------------------------------------

// mpTitle is one title in any list (Title in the extension).
type mpTitle struct {
	ID       int
	Name     string
	Author   string
	Portrait string
	Language int
}

func mpReadTitle(m mpMsg) mpTitle {
	return mpTitle{ID: int(m.num(1)), Name: m.str(2), Author: m.str(3), Portrait: m.str(4), Language: int(m.num(7))}
}

func (t mpTitle) manga() sourcekit.Manga {
	id := strconv.Itoa(t.ID)
	return sourcekit.Manga{URL: "#/titles/" + id, ID: id, Title: t.Name, CoverURL: t.Portrait}
}

// call fetches one API endpoint and returns its success result, or the
// error the API explains itself with.
func (m *mangaplus) call(ctx context.Context, path string, q url.Values) (mpMsg, error) {
	data, err := m.c.Do(ctx, sourcekit.Request{URL: m.api + path, Query: q,
		Headers: map[string]string{"SESSION-TOKEN": m.session, "Referer": m.site + "/"}})
	if err != nil {
		return nil, err
	}
	resp, err := mpParse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if ok := resp.msg(1); ok != nil {
		return ok, nil
	}
	// the error carries a popup per language; Spanish has its own
	var subject, body string
	if e := resp.msg(2); e != nil {
		p := e.msg(2)
		if _, code := mpLang(m.code); code == 1 && e.msg(3) != nil {
			p = e.msg(3)
		}
		if p != nil {
			subject, body = p.str(1), p.str(2)
		}
	}
	if subject == "Not Found" {
		return nil, fmt.Errorf("%w: %s", sourcekit.ErrNotFound, mpFirst(body, subject))
	}
	return nil, fmt.Errorf("MANGA Plus: %s", mpFirst(body, "the API answered with an error"))
}

func mpFirst(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// forLang keeps the titles of this catalog's language, once each.
func (m *mangaplus) forLang(titles []mpTitle) []mpTitle {
	_, code := mpLang(m.code)
	seen := map[int]bool{}
	var out []mpTitle
	for _, t := range titles {
		if t.ID == 0 || t.Language != code || seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		out = append(out, t)
	}
	return out
}

func mpResults(titles []mpTitle) sourcekit.Results {
	res := sourcekit.Results{Mangas: make([]sourcekit.Manga, 0, len(titles))}
	for _, t := range titles {
		res.Mangas = append(res.Mangas, t.manga())
	}
	return res
}

// ---- browse and search ------------------------------------------------------

// The API answers every list in one go: there is never a second page.

func (m *mangaplus) Popular(ctx context.Context, page int) (sourcekit.Results, error) {
	if page > 1 {
		return sourcekit.Results{}, nil
	}
	lang, _ := mpLang(m.code)
	ok, err := m.call(ctx, "/title_list/rankingV2", url.Values{"lang": {lang}, "type": {"hottest"}, "clang": {lang}})
	if err != nil {
		return sourcekit.Results{}, err
	}
	var titles []mpTitle
	for _, ranked := range ok.msg(37).msgs(3) {
		for _, t := range ranked.msgs(2) {
			titles = append(titles, mpReadTitle(t))
		}
	}
	return mpResults(m.forLang(titles)), nil
}

func (m *mangaplus) Latest(ctx context.Context, page int) (sourcekit.Results, error) {
	if page > 1 {
		return sourcekit.Results{}, nil
	}
	lang, _ := mpLang(m.code)
	ok, err := m.call(ctx, "/web/web_homeV4", url.Values{"lang": {lang}, "clang": {lang}})
	if err != nil {
		return sourcekit.Results{}, err
	}
	home := ok.msg(38)
	updated := []mpMsg{}
	for _, g := range home.msgs(2) {
		updated = append(updated, g.msgs(2)...)
	}
	if f := home.msg(7).msg(2); f != nil {
		updated = append(updated, f)
	}
	sort.SliceStable(updated, func(i, j int) bool { return updated[i].num(6) > updated[j].num(6) })
	var titles []mpTitle
	for _, u := range updated {
		for _, latest := range u.msgs(3) {
			titles = append(titles, mpReadTitle(latest.msg(1)))
		}
	}
	return mpResults(m.forLang(titles)), nil
}

// Search matches the query against every title's name and author, as the
// extension does: the API has no search of its own.
func (m *mangaplus) Search(ctx context.Context, query string, page int) (sourcekit.Results, error) {
	if page > 1 {
		return sourcekit.Results{}, nil
	}
	query = strings.TrimSpace(query)
	var titles []mpTitle
	if query != "" {
		// allV2 has every title, finished ones included
		ok, err := m.call(ctx, "/title_list/allV2", nil)
		if err != nil {
			return sourcekit.Results{}, err
		}
		for _, g := range ok.msg(25).msgs(1) {
			for _, t := range g.msgs(2) {
				titles = append(titles, mpReadTitle(t))
			}
		}
	} else {
		lang, _ := mpLang(m.code)
		ok, err := m.call(ctx, "/title_list/all_v3", url.Values{"type": {"serializing"}, "lang": {lang}, "clang": {lang}})
		if err != nil {
			return sourcekit.Results{}, err
		}
		for _, e := range ok.msg(35).msgs(3) {
			titles = append(titles, mpReadTitle(e.msg(2)))
		}
	}
	titles = m.forLang(titles)
	q := strings.ToLower(query)
	var found []mpTitle
	for _, t := range titles {
		if q == "" || strings.Contains(strings.ToLower(t.Name), q) || strings.Contains(strings.ToLower(t.Author), q) {
			found = append(found, t)
		}
	}
	return mpResults(found), nil
}

// ---- one title ----------------------------------------------------------------

var (
	mpCompleted = regexp.MustCompile(`(?i)completado|completed?|completo`)
	mpHiatus    = regexp.MustCompile(`(?i)on a hiatus`)
)

// detail fetches a title's page, refusing one in another language (the API
// answers for any title id whatever the language asked for).
func (m *mangaplus) detail(ctx context.Context, ref sourcekit.Ref) (mpMsg, mpTitle, error) {
	id := mpLastID(ref.URL, ref.ID)
	if id == "" {
		return nil, mpTitle{}, fmt.Errorf("%q is not a title url", ref.URL)
	}
	lang, code := mpLang(m.code)
	ok, err := m.call(ctx, "/title_detailV3", url.Values{"title_id": {id}, "clang": {lang}})
	if err != nil {
		return nil, mpTitle{}, err
	}
	view := ok.msg(8)
	t := mpReadTitle(view.msg(1))
	if view == nil || t.Language != code {
		return nil, mpTitle{}, fmt.Errorf("%w: title %s is not available in this language", sourcekit.ErrNotFound, id)
	}
	return view, t, nil
}

func (m *mangaplus) Details(ctx context.Context, ref sourcekit.Ref) (sourcekit.Details, error) {
	view, t, err := m.detail(ctx, ref)
	if err != nil {
		return sourcekit.Details{}, err
	}
	author := strings.ReplaceAll(t.Author, " / ", ", ")
	d := sourcekit.Details{Manga: t.manga(), Author: author, Artist: author, Status: sourcekit.StatusOngoing,
		WebURL: m.site + "/titles/" + strconv.Itoa(t.ID)}
	overview, period, nonAppearance := view.str(3), view.str(7), view.str(8)
	var desc []string
	for _, s := range []string{overview, period} {
		if s != "" {
			desc = append(desc, s)
		}
	}
	d.Description = strings.Join(desc, "\n\n")
	oneShot := false
	for _, g := range view.msgs(31) {
		if name := g.str(1); name != "" {
			d.Genres = append(d.Genres, name)
		}
		if g.str(2) == "one-shot" {
			oneShot = true
		}
	}
	switch {
	case oneShot || mpCompleted.MatchString(nonAppearance) || strings.Contains(period, "latest 0 chapters"):
		d.Status = sourcekit.StatusCompleted
	case mpHiatus.MatchString(nonAppearance):
		d.Status = sourcekit.StatusHiatus
	}
	return d, nil
}

func (m *mangaplus) Chapters(ctx context.Context, ref sourcekit.Ref) ([]sourcekit.Chapter, error) {
	view, _, err := m.detail(ctx, ref)
	if err != nil {
		return nil, err
	}
	var all []mpMsg
	for _, g := range view.msgs(28) {
		all = append(all, g.msgs(2)...)
		all = append(all, g.msgs(4)...)
	}
	out := make([]sourcekit.Chapter, 0, len(all))
	// the API lists oldest first; newest first is what a chapter list shows
	for i := len(all) - 1; i >= 0; i-- {
		c := all[i]
		sub, hasSub := c.has(4)
		if !hasSub {
			continue // expired: no longer readable
		}
		name := c.str(3)
		id := strconv.Itoa(int(c.num(2)))
		ch := sourcekit.Chapter{URL: "#/viewer/" + id, ID: id, Scanlator: "MANGA Plus", Number: mpChapterNumber(name),
			WebURL: m.site + "/viewer/" + id}
		if m.subtitleOnly {
			ch.Name = sub
		} else {
			ch.Name = name + " - " + sub
		}
		if at := c.num(6); at > 0 {
			t := time.Unix(at, 0).UTC()
			ch.UploadedAt = &t
		}
		out = append(out, ch)
	}
	return out, nil
}

// mpChapterNumber reads "#012" as 12 (-1 for "Ex" and friends).
func mpChapterNumber(name string) float64 {
	if _, after, ok := strings.Cut(name, "#"); ok {
		name = after
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(name), 64)
	if err != nil {
		return -1
	}
	return n
}

func (m *mangaplus) Pages(ctx context.Context, ch sourcekit.PageRef) ([]sourcekit.PageImage, error) {
	id := mpLastID(ch.URL, ch.ID)
	if id == "" {
		return nil, fmt.Errorf("%q is not a chapter url", ch.URL)
	}
	lang, _ := mpLang(m.code)
	split := "no"
	if m.split {
		split = "yes"
	}
	q := url.Values{"chapter_id": {id}, "split": {split}, "img_quality": {m.quality}, "clang": {lang}}
	ok, err := m.call(ctx, "/manga_viewer_v3", q)
	if err != nil {
		return nil, err
	}
	viewer := ok.msg(10)
	token := viewer.str(19)
	var pages []sourcekit.PageImage
	for _, p := range viewer.msgs(1) {
		mp := p.msg(1)
		if mp == nil {
			continue // an ad or the "last page" card, not a manga page
		}
		img := mp.str(1)
		if img == "" {
			continue
		}
		pages = append(pages, sourcekit.PageImage{Index: len(pages), URL: img, Decode: mp.str(5),
			Headers: map[string]string{"Plus-Vw-Token": token, "Referer": m.site + "/"}})
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: the chapter has no pages", sourcekit.ErrNotFound)
	}
	return pages, nil
}

// DecodePage undoes the page's encryption: every byte XORed with the hex
// key the viewer handed out, repeated over the whole image.
func (m *mangaplus) DecodePage(_ context.Context, decode string, data []byte) ([]byte, error) {
	return mpXOR(decode, data)
}

func mpXOR(hexKey string, data []byte) ([]byte, error) {
	key, err := hex.DecodeString(strings.TrimSpace(hexKey))
	if err != nil {
		return nil, fmt.Errorf("page key: %w", err)
	}
	if len(key) == 0 {
		return nil, errors.New("page key is empty")
	}
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = b ^ key[i%len(key)]
	}
	return out, nil
}

// mpLastID is the id at the end of "#/titles/<id>", "#/viewer/<id>" or the
// site's own links, falling back to the id stored with the link.
func mpLastID(raw, stored string) string {
	s, _, _ := strings.Cut(strings.TrimSpace(raw), "?")
	s = strings.TrimRight(s, "/")
	s = s[strings.LastIndex(s, "/")+1:]
	s = strings.TrimPrefix(s, "#")
	if _, err := strconv.Atoi(s); err == nil {
		return s
	}
	return stored
}

// ---- protobuf -----------------------------------------------------------------

// mpMsg is one decoded protobuf message: its fields by number, in order.
// Only what the API's messages use is supported: varints, length-delimited
// fields (strings and nested messages), and fixed-size fields to skip.
type mpMsg map[int][]mpValue

type mpValue struct {
	num   uint64
	bytes []byte
	isLen bool
}

func mpParse(b []byte) (mpMsg, error) {
	m := mpMsg{}
	for len(b) > 0 {
		tag, n := mpVarint(b)
		if n <= 0 {
			return nil, errors.New("protobuf: bad field tag")
		}
		b = b[n:]
		field, wire := int(tag>>3), tag&7
		if field <= 0 {
			return nil, errors.New("protobuf: bad field number")
		}
		switch wire {
		case 0:
			v, n := mpVarint(b)
			if n <= 0 {
				return nil, errors.New("protobuf: bad varint")
			}
			b = b[n:]
			m[field] = append(m[field], mpValue{num: v})
		case 1:
			if len(b) < 8 {
				return nil, errors.New("protobuf: short fixed64")
			}
			b = b[8:]
		case 2:
			l, n := mpVarint(b)
			if n <= 0 || uint64(len(b)-n) < l {
				return nil, errors.New("protobuf: bad length")
			}
			b = b[n:]
			m[field] = append(m[field], mpValue{bytes: b[:l], isLen: true})
			b = b[l:]
		case 5:
			if len(b) < 4 {
				return nil, errors.New("protobuf: short fixed32")
			}
			b = b[4:]
		default:
			return nil, fmt.Errorf("protobuf: unsupported wire type %d", wire)
		}
	}
	return m, nil
}

func mpVarint(b []byte) (uint64, int) {
	var v uint64
	for i := 0; i < len(b) && i < 10; i++ {
		v |= uint64(b[i]&0x7f) << (7 * i)
		if b[i] < 0x80 {
			return v, i + 1
		}
	}
	return 0, 0
}

func (m mpMsg) last(f int) (mpValue, bool) {
	vs := m[f]
	if len(vs) == 0 {
		return mpValue{}, false
	}
	return vs[len(vs)-1], true
}

// str is a string field ("" when absent).
func (m mpMsg) str(f int) string {
	s, _ := m.has(f)
	return s
}

// has is a string field and whether it was sent at all, for the fields
// whose absence means something (an expired chapter has no subtitle).
func (m mpMsg) has(f int) (string, bool) {
	v, ok := m.last(f)
	if !ok || !v.isLen {
		return "", false
	}
	return string(v.bytes), true
}

// num is an int32 field, as the API declares its numbers.
func (m mpMsg) num(f int) int64 {
	v, ok := m.last(f)
	if !ok || v.isLen {
		return 0
	}
	return int64(int32(v.num))
}

// msg is a nested message (nil when absent or unreadable).
func (m mpMsg) msg(f int) mpMsg {
	if m == nil {
		return nil
	}
	v, ok := m.last(f)
	if !ok || !v.isLen {
		return nil
	}
	out, err := mpParse(v.bytes)
	if err != nil {
		return nil
	}
	return out
}

// msgs is a repeated nested message.
func (m mpMsg) msgs(f int) []mpMsg {
	if m == nil {
		return nil
	}
	var out []mpMsg
	for _, v := range m[f] {
		if !v.isLen {
			continue
		}
		if x, err := mpParse(v.bytes); err == nil {
			out = append(out, x)
		}
	}
	return out
}
