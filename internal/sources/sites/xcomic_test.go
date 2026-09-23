package sites

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// xcomicProbes are the uploads of title "t1": two English, one French, and
// one taken down.
var xcomicProbes = map[string]string{
	"c-en":   `{"name":"Berserk","dbStatus":"normal","isPublic":true,"translatedLanguage":"en","chaps_normal":120,"urlPath":"/source/c-en","urlCover":"/covers/c-en.jpg"}`,
	"c-en2":  `{"name":"Berserk","subName":"Fan &amp; Friends","translatedLanguage":"en","chaps_normal":5}`,
	"c-fr":   `{"name":"Berserk","translatedLanguage":"fr","chaps_normal":300}`,
	"c-dead": `{"name":"Berserk","dbStatus":"deleted","translatedLanguage":"en","chaps_normal":999}`,
}

type xcomicServer struct {
	t      *testing.T
	mu     sync.Mutex
	probes int
	pages  []int
}

func (s *xcomicServer) query(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query     string          `json:"query"`
		Variables json.RawMessage `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || r.Method != http.MethodPost {
		s.t.Errorf("%s: %v", r.Method, err)
		return
	}
	var vars struct {
		ID     string         `json:"id"`
		Select map[string]any `json:"select"`
	}
	_ = json.Unmarshal(req.Variables, &vars)
	data := func(v string) { _, _ = w.Write([]byte(`{"data":` + v + `}`)) }
	s.mu.Lock()
	defer s.mu.Unlock()
	switch q := req.Query; {
	case strings.Contains(q, "get_title_browse_items"):
		sel := vars.Select
		if sel["size"] != 12.0 || sel["where"] != "browse" || sel["page"] != 1.0 || sel["init"] != 0.0 || sel["sortby"] != "field_score" {
			s.t.Errorf("browse select %v", sel)
		}
		if w, ok := sel["word"]; ok && w != "berserk" {
			s.t.Errorf("browse word %v", w)
		}
		data(`{"get_title_browse_items":[{"id":"t1","data":{"title":"Berserk","cover_local_url":"/covers/t1.jpg",
			"comic_ids":["c-en","c-fr","c-dead","c-en2",""],"chap_last_public_at":1700000000000}}]}`)
	case strings.Contains(q, "get_comicNode") && strings.Contains(q, "originalStatus"):
		if vars.ID != "c-en" {
			s.t.Errorf("full upload %q", vars.ID)
		}
		data(`{"get_comicNode":{"id":"c-en","data":{"id":"c-en","name":"Berserk (Official)","translatedLanguage":"en",
			"originalStatus":"completed","uploadStatus":"ongoing","type":"manga","genres":["dark_fantasy","action"],
			"contentRating":"suggestive","summary":{"text":"Upload summary."},"urlPath":"/source/c-en-berserk","urlCover":"/covers/c-en.jpg","chaps_normal":120}}}`)
	case strings.Contains(q, "get_comicNode"):
		s.probes++
		p, ok := xcomicProbes[vars.ID]
		if !ok {
			data(`{"get_comicNode":null}`)
			return
		}
		data(`{"get_comicNode":{"id":"` + vars.ID + `","data":` + p + `}}`)
	case strings.Contains(q, "get_title_titleNode"):
		if vars.ID != "t1" {
			data(`{"get_title_titleNode":null}`)
			return
		}
		data(`{"get_title_titleNode":{"id":"t1","data":{"title":"Berserk","authors":["Kentaro Miura"],"artists":["Kentaro Miura"],
			"type":"manga","description":"Guts, the Black Swordsman.","cover_local_url":"/covers/t1.jpg","genre_ids":["action","horror"],
			"demographic_ids":["seinen"],"comic_ids":["c-en","c-fr","c-dead","c-en2"],"chap_last_public_at":1700000000000}}}`)
	case strings.Contains(q, "get_comic_chapterList_uniqList"):
		if vars.Select["comic_id"] != "c-en" || vars.Select["size"] != 1000.0 {
			s.t.Errorf("chapter list select %v", vars.Select)
		}
		data(`{"get_comic_chapterList_uniqList":{"paging":{"next":0,"total":2},"items":[
			{"id":"k2","data":{"id":"k2","chaNum":2,"dname":"Ch.2","title":"The Brand","dateModify":1700000000000,"srcName":"mangadex","urlPath":"/title/t1/k2"}},
			{"id":"k1","data":{"id":"k1","serial":1.5,"dname":"Chapter 1.5","dateCreate":1690000000000,
			 "profileNodes":[{"data":{"name":"Evil Genius"}},null,{"data":null}]}}]}}`)
	case strings.Contains(q, "get_comic_chapterList_fullList"):
		page := int(vars.Select["page"].(float64))
		s.pages = append(s.pages, page)
		data(`{"get_comic_chapterList_fullList":{"paging":{"next":` + map[bool]string{true: "2", false: "0"}[page == 1] + `,"total":150},
			"items":[{"id":"p` + string(rune('0'+page)) + `","data":{"id":"p` + string(rune('0'+page)) + `","chaNum":` + string(rune('0'+page)) + `}}]}}`)
	case strings.Contains(q, "get_chapterNode"):
		if vars.ID != "k2" {
			s.t.Errorf("pages of %q", vars.ID)
		}
		data(`{"get_chapterNode":{"id":"k2","data":{"imageUrls":["/media/1.webp","https://cdn.example/2.webp"]}}}`)
	default:
		_, _ = w.Write([]byte(`{"errors":[{"message":"unknown query"}]}`))
	}
}

func newXComic(t *testing.T, lang string) (*xcomic, *xcomicServer) {
	t.Helper()
	s := &xcomicServer{t: t}
	mux := http.NewServeMux()
	mux.HandleFunc("/query/", s.query)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &xcomic{c: sourcekit.NewClient(srv.Client()), base: srv.URL, lang: lang, dedupe: true,
		probes: map[string]xcomicProbe{}, fresh: map[string]int64{}}, s
}

// TestXComic walks a series the way the library does: find it, read its
// details, list chapters, then a chapter's pages.
func TestXComic(t *testing.T) {
	x, srv := newXComic(t, "en")
	ctx := context.Background()

	res, err := x.Search(ctx, "berserk", 1)
	if err != nil || len(res.Mangas) != 2 || res.HasNext {
		t.Fatalf("search: %v %+v", err, res)
	}
	// one row per live English upload, the fullest first
	if got := res.Mangas[0]; got.URL != "t1:c-en" || got.Title != "Berserk" || got.CoverURL != x.base+"/covers/t1.jpg" ||
		got.Chapters == nil || *got.Chapters != 120 {
		t.Fatalf("first result %+v", got)
	}
	if got := res.Mangas[1]; got.URL != "t1:c-en2" || got.Title != "Berserk · Fan & Friends" {
		t.Fatalf("second result %+v", got)
	}
	if srv.probes != 4 {
		t.Fatalf("%d probes", srv.probes)
	}
	// the title has no newer chapter, so its uploads aren't looked at again
	if _, err := x.Popular(ctx, 1); err != nil || srv.probes != 4 {
		t.Fatalf("popular: %v, %d probes", err, srv.probes)
	}

	d, err := x.Details(ctx, sourcekit.Ref{URL: "t1:c-en"})
	if err != nil || d.URL != "t1:c-en" || d.Title != "Berserk" || d.Author != "Kentaro Miura" || d.Status != sourcekit.StatusOngoing ||
		d.Description != "Guts, the Black Swordsman." || d.CoverURL != x.base+"/covers/t1.jpg" || d.WebURL != x.base+"/source/c-en-berserk" ||
		strings.Join(d.Genres, ",") != "Manga,Suggestive,Dark Fantasy,Action,Seinen,Horror" {
		t.Fatalf("details: %v %+v", err, d)
	}
	// a title's bare id (no upload pinned) picks the fullest upload
	if d, err := x.Details(ctx, sourcekit.Ref{URL: "t1"}); err != nil || d.URL != "t1" || d.Title != "Berserk" {
		t.Fatalf("unpinned details: %v %+v", err, d)
	}

	chs, err := x.Chapters(ctx, sourcekit.Ref{URL: "t1:c-en"})
	if err != nil || len(chs) != 2 {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
	if c := chs[0]; c.URL != "k2" || c.Name != "Ch.2: The Brand" || c.Number != 2 || c.Scanlator != "Mangadex" ||
		c.UploadedAt == nil || c.UploadedAt.UnixMilli() != 1700000000000 || c.WebURL != x.base+"/title/t1/k2" {
		t.Fatalf("first chapter %+v", c)
	}
	if c := chs[1]; c.Name != "Chapter 1.5" || c.Number != 1.5 || c.Scanlator != "Evil Genius" || c.WebURL != x.base+"/chapter/k1" {
		t.Fatalf("second chapter %+v", c)
	}
	if chs, err := x.Chapters(ctx, sourcekit.Ref{URL: "t1"}); err != nil || len(chs) != 2 {
		t.Fatalf("unpinned chapters: %v %+v", err, chs)
	}

	pages, err := x.Pages(ctx, sourcekit.PageRef{URL: "k2"})
	if err != nil || len(pages) != 2 {
		t.Fatalf("pages: %v %+v", err, pages)
	}
	if pages[0].URL != x.base+"/media/1.webp" || pages[1].URL != "https://cdn.example/2.webp" || pages[0].Headers["Referer"] == "" {
		t.Fatalf("pages %+v", pages)
	}
}

// TestXComicAll: the "all" catalog lists every language's uploads, naming
// the language.
func TestXComicAll(t *testing.T) {
	x, _ := newXComic(t, "all")
	res, err := x.Search(context.Background(), "", 1)
	if err != nil || len(res.Mangas) != 3 {
		t.Fatalf("search: %v %+v", err, res)
	}
	if res.Mangas[0].URL != "t1:c-fr" || res.Mangas[0].Title != "Berserk [FR]" {
		t.Fatalf("first result %+v", res.Mangas[0])
	}
}

// TestXComicChapterPaging: without the site's deduplication the list comes
// in pages of 100, all of them read.
func TestXComicChapterPaging(t *testing.T) {
	x, srv := newXComic(t, "en")
	x.dedupe = false
	chs, err := x.Chapters(context.Background(), sourcekit.Ref{URL: "t1:c-en"})
	if err != nil || len(chs) != 2 || chs[1].URL != "p2" {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
	if len(srv.pages) != 2 {
		t.Fatalf("pages read %v", srv.pages)
	}
	if got := x.siteLang(); got != "en" {
		t.Fatalf("site language %q", got)
	}
	if got := (&xcomic{lang: "zh-Hant"}).siteLang(); got != "zh_hk" {
		t.Fatalf("zh-Hant asks the site for %q", got)
	}
}
