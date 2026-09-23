package sites

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// The markup below is trimmed to what the extension reads. Lists are
// "div.relative.group" cards (an 18+ one under an overlay); the manga page
// carries its chapter list's Livewire snapshot; chapters are links, or a
// dropdown of links when several groups translated one.
const onisListHTML = `<html><head><meta name="csrf-token" content="csrf1"></head><body>
<div wire:snapshot='{"memo":{"name":"post-filter"}}'>
<div class="relative group"><a href="/manga/berserk"><img alt="Berserk" data-src="/covers/berserk.webp" src="data:image/gif;base64,xx"></a><h3>Berserk</h3></div>
<div class="relative group"><div class="absolute inset-0 z-20"><span>18+</span></div><a href="/manga/adult"><img alt="Adult" src="/covers/adult.webp"></a><h3>Adult</h3></div>
<button wire:click="nextPage('page')">Next</button>
</div></body></html>`

const onisPageTwoHTML = `<div class="relative group"><a href="/manga/vagabond"><img alt="Vagabond" src="/covers/v.webp"></a><h3>Vagabond</h3></div>
<button wire:click="nextPage('page')" disabled>Next</button>`

const onisMangaHTML = `<html><head><meta name="csrf-token" content="csrf1"></head><body>
<div wire:snapshot='{"memo":{"name":"manga.chapter-list"}}'></div>
<h1>Berserk</h1>
<div class="w-32"><picture><source srcset="/a.avif"><source srcset="/a.webp"><img src="/covers/berserk.webp"></picture></div>
<div class="flex items-center gap-2 justify-center mb-2">
  <div data-flux-badge>Manhwa</div><div data-flux-badge>Webtoon</div>
  <span class="inline-flex"><span class="size-1.5"></span>Ongoing</span>
</div>
<div class="flex flex-col md:flex-row"><a href="/author/miura">Kentaro Miura</a><a href="/genre/action">Action</a></div>
<p class="leading-relaxed">Guts, the Black Swordsman.</p>
</body></html>`

const onisChapterTwo = `<a class="flex gap-4" href="/read/berserk/ch-2"><div data-flux-heading>Chapter 2</div><p data-flux-text>EN - 3 days ago · 20 pages</p></a>`

const onisChapterOne = `<ui-dropdown><button><div data-flux-heading>Chapter 1</div><p data-flux-text>2 weeks ago</p></button>
<ui-menu><a data-flux-menu-item href="/read/berserk/ch-1a"><span class="text-sm">Group A</span></a>
<a data-flux-menu-item href="/read/berserk/ch-1b"><span class="text-sm">Unknown group</span></a></ui-menu></ui-dropdown>`

const onisReaderHTML = `<html><body><script>window.reader = {readerToken: "t0", pages: [{order: 0}, {order: 1}]};</script></body></html>`

type onisServer struct {
	t     *testing.T
	mu    sync.Mutex
	next  string   // the page token the API accepts next
	calls []string // Livewire methods called, with their filters
	langs []string // chapter languages asked for
}

func newOniSaga(t *testing.T, lang string) (*onisaga, *onisServer) {
	t.Helper()
	s := &onisServer{t: t, next: "t0"}
	mux := http.NewServeMux()
	serve := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }
	}
	mux.HandleFunc("/browse", serve(onisListHTML))
	mux.HandleFunc("/search/berserk", serve(onisListHTML))
	mux.HandleFunc("/manga/berserk", serve(onisMangaHTML))
	mux.HandleFunc("/read/berserk/ch-2", serve(onisReaderHTML))
	mux.HandleFunc("/img/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") == "" {
			t.Error("images want the reader as referer")
		}
		_, _ = w.Write([]byte("IMG:" + r.URL.Path))
	})
	mux.HandleFunc("/livewire/update", s.livewire)
	mux.HandleFunc("/api/chapter/ch-2/page/{n}", s.page)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &onisaga{c: sourcekit.NewClient(srv.Client()), base: srv.URL, lang: lang, pageDelay: time.Millisecond,
		tokens: map[string]string{}}, s
}

func (s *onisServer) livewire(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token      string `json:"_token"`
		Components []struct {
			Snapshot string          `json:"snapshot"`
			Updates  json.RawMessage `json:"updates"`
			Calls    []onisCall      `json:"calls"`
		} `json:"components"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Components) != 1 || len(req.Components[0].Calls) != 1 {
		s.t.Errorf("livewire request: %v %+v", err, req)
		return
	}
	if req.Token != "csrf1" || r.Header.Get("X-Requested-With") != "XMLHttpRequest" || r.Header.Get("Referer") == "" {
		s.t.Errorf("livewire headers %v token %q", r.Header, req.Token)
	}
	c := req.Components[0]
	answer := func(snapshot, html string) {
		_ = json.NewEncoder(w).Encode(map[string]any{"components": []any{map[string]any{"snapshot": snapshot, "effects": map[string]any{"html": html}}}})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch c.Calls[0].Method {
	case "gotoPage":
		s.calls = append(s.calls, "gotoPage "+strings.Join(c.Calls[0].Params, ",")+" "+string(c.Updates))
		answer("next", onisPageTwoHTML)
	case "loadMoreChapters":
		var u struct {
			Language string `json:"language"`
		}
		_ = json.Unmarshal(c.Updates, &u)
		s.langs = append(s.langs, u.Language)
		// the list grows by one load, then stops
		switch c.Snapshot {
		case `{"memo":{"name":"manga.chapter-list"}}`:
			answer("s2", onisChapterTwo)
		case "s2":
			answer("s3", onisChapterTwo+onisChapterOne)
		default:
			answer("s4", onisChapterTwo+onisChapterOne)
		}
	default:
		s.t.Errorf("livewire method %q", c.Calls[0].Method)
	}
}

// page is the page API: each answer hands on the token for the next call.
func (s *onisServer) page(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	got := r.Header.Get("X-Reader-Token")
	if got != s.next {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Reader token expired"}`))
		return
	}
	s.next = got + "+"
	w.Header().Set("X-Reader-Token-Next", s.next)
	_, _ = w.Write([]byte(`{"url":"/img/` + r.PathValue("n") + `.webp","order":` + r.PathValue("n") + `}`))
}

// TestOniSaga walks a series the way the library does: find it, read its
// details, list chapters, then fetch a chapter's pages through the token
// chain of the page API.
func TestOniSaga(t *testing.T) {
	o, srv := newOniSaga(t, "en")
	ctx := context.Background()

	res, err := o.Search(ctx, "berserk", 1)
	if err != nil || len(res.Mangas) != 1 || !res.HasNext {
		t.Fatalf("search: %v %+v", err, res)
	}
	if got := res.Mangas[0]; got.URL != "/manga/berserk" || got.Title != "Berserk" || got.CoverURL != o.base+"/covers/berserk.webp" {
		t.Fatalf("result %+v", got)
	}
	o.nsfw = true
	if res, _ := o.Search(ctx, "berserk", 1); len(res.Mangas) != 2 || res.Mangas[1].Title != "Adult" {
		t.Fatalf("18+ titles are listed when asked for: %+v", res)
	}

	pop, err := o.Popular(ctx, 2)
	if err != nil || len(pop.Mangas) != 1 || pop.Mangas[0].URL != "/manga/vagabond" || pop.HasNext {
		t.Fatalf("popular: %v %+v", err, pop)
	}
	if len(srv.calls) != 1 || !strings.HasPrefix(srv.calls[0], `gotoPage 2 {"platform":"","status":"","sort":"view","min_chapters":"","group":null,`) {
		t.Fatalf("livewire calls %v", srv.calls)
	}

	d, err := o.Details(ctx, sourcekit.Ref{URL: "/manga/berserk"})
	if err != nil || d.Title != "Berserk" || d.Author != "Kentaro Miura" || d.Status != sourcekit.StatusOngoing ||
		d.CoverURL != o.base+"/covers/berserk.webp" || strings.Join(d.Genres, ",") != "Manhwa,Action" ||
		d.Description != "Guts, the Black Swordsman." {
		t.Fatalf("details: %v %+v", err, d)
	}

	chs, err := o.Chapters(ctx, sourcekit.Ref{URL: "/manga/berserk"})
	if err != nil || len(chs) != 3 {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
	if c := chs[0]; c.URL != "/read/berserk/ch-2" || c.Name != "Chapter 2" || c.Number != 2 || c.Scanlator != "" ||
		c.UploadedAt == nil || time.Since(*c.UploadedAt) < 71*time.Hour {
		t.Fatalf("first chapter %+v", c)
	}
	if chs[1].Scanlator != "Group A" || chs[2].Scanlator != "Unknown 1" || chs[2].Number != 1 {
		t.Fatalf("dropdown chapters %+v", chs[1:])
	}
	if strings.Join(srv.langs, ",") != "EN,EN,EN" {
		t.Fatalf("chapter loads %v", srv.langs)
	}

	pages, err := o.Pages(ctx, sourcekit.PageRef{URL: chs[0].URL})
	if err != nil || len(pages) != 2 || pages[0].URL != o.base+"/read/berserk/ch-2" || pages[1].Decode != "1 /read/berserk/ch-2" {
		t.Fatalf("pages: %v %+v", err, pages)
	}
	for i, p := range pages {
		img, err := o.DecodePage(ctx, p.Decode, fetchForTest(t, p))
		if err != nil || string(img) != "IMG:/img/"+string(rune('0'+i))+".webp" {
			t.Fatalf("page %d: %v %q", i, err, img)
		}
	}
}

// TestOniSagaRefusedToken: when the token the site last handed on is
// refused, the page's own fresh token is tried, then a new one.
func TestOniSagaRefusedToken(t *testing.T) {
	o, srv := newOniSaga(t, "en")
	ctx := context.Background()
	pages, err := o.Pages(ctx, sourcekit.PageRef{URL: "/read/berserk/ch-2"})
	if err != nil || len(pages) != 2 {
		t.Fatalf("pages: %v %+v", err, pages)
	}
	o.setToken("ch-2", "stale")
	img, err := o.DecodePage(ctx, pages[0].Decode, fetchForTest(t, pages[0]))
	if err != nil || string(img) != "IMG:/img/0.webp" {
		t.Fatalf("page: %v %q", err, img)
	}
	if o.token("ch-2") != "t0+" || srv.next != "t0+" {
		t.Fatalf("the next token is kept: %q", o.token("ch-2"))
	}
	if _, err := o.DecodePage(ctx, "x /read/berserk/ch-2", nil); err == nil {
		t.Fatal("a bad page number should be refused")
	}
}

// TestOniSagaAll: the "all" catalog lists every language's chapters, the
// language in front of the group.
func TestOniSagaAll(t *testing.T) {
	o, srv := newOniSaga(t, "all")
	chs, err := o.Chapters(context.Background(), sourcekit.Ref{URL: "/manga/berserk"})
	if err != nil || len(chs) != 3 {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
	if chs[0].Scanlator != "EN" || chs[1].Scanlator != "EN - Group A" {
		t.Fatalf("chapters %+v", chs)
	}
	if len(srv.langs) != 3*len(onisChapterLangs) {
		t.Fatalf("chapter loads %v", srv.langs)
	}
}

// fetchForTest fetches a page the way the native module does before it
// hands the bytes to DecodePage.
func fetchForTest(t *testing.T, p sourcekit.PageImage) []byte {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, p.URL, nil)
	for k, v := range p.Headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return b
}
