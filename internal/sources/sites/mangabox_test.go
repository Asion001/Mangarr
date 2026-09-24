package sites

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// The markup below has the shapes the extension reads. The network has two
// layouts: "kakalot" (a .manga-info-top list of "Label : value" rows) and
// "nelo" (a .panel-story-info table of label and value cells).
const (
	mboxHotHTML = `
<div class="truyen-list">
  <div class="list-truyen-item-wrap">
    <a class="list-story-item" href="https://www.mangakakalot.gg/manga/solo-leveling"><img src="https://cdn.example/solo.jpg"></a>
    <h3><a href="https://www.mangakakalot.gg/manga/solo-leveling">Solo Leveling</a></h3>
    <a class="list-story-item-wrap-chapter" data-id="99" href="/manga/solo-leveling/chapter-200">Chapter 200</a></div>
  <div class="list-truyen-item-wrap"><h3><a href="/manga/an-ad">Not a manga</a></h3></div>
</div>
<div class="group_page"><a class="page_select">1</a><a href="?page=2">2</a></div>`

	mboxSearchHTML = `
<div class="panel_story_list">
  <div class="story_item"><a href="/manga/solo-leveling"><img src="/covers/solo.jpg"></a>
    <div class="story_item_right"><h3 class="story_name"><a href="/manga/solo-leveling">Solo Leveling</a></h3></div></div>
</div>
<div class="panel_page_number"><a class="page_select">1</a><a class="page_last">Last(1)</a></div>`

	mboxKakalotHTML = `
<div class="manga-info-top">
  <div class="manga-info-pic"><img src="https://cdn.example/solo.jpg"></div>
  <ul class="manga-info-text">
    <li><h1>Solo Leveling</h1><h2 class="story-alternative">Na Honjaman Level Up</h2></li>
    <li>Author(s) : <a href="/author/1">Chugong</a>, <a href="/author/2">DUBU</a></li>
    <li>Status : Completed</li>
    <li class="genres">Genres : <a href="/genre/action">Action</a>, <a href="/genre/fantasy">Fantasy</a></li>
  </ul>
</div>
<div id="contentBox"><h2><p>Solo Leveling summary:</p></h2>Solo Leveling summary: A weak hunter.</div>`

	mboxNeloHTML = `
<div class="panel-story-info">
  <div class="story-info-left"><span class="info-image"><img src="/covers/solo.jpg"></span></div>
  <div class="story-info-right"><h1>Solo Leveling</h1>
  <table class="variations-tableInfo"><tbody>
    <tr><td class="table-label"><i class="info-alternative"></i>Alternative :</td><td class="table-value"><h2>Only I Level Up</h2></td></tr>
    <tr><td class="table-label"><i class="info-author"></i>Author(s) :</td><td class="table-value"><a href="/a/1">Chugong</a></td></tr>
    <tr><td class="table-label"><i class="info-status"></i>Status :</td><td class="table-value">Ongoing</td></tr>
    <tr><td class="table-label"><i class="info-genres"></i>Genres :</td><td class="table-value"><a>Action</a> - <a>Fantasy</a></td></tr>
  </tbody></table></div>
</div>
<div class="panel-story-info-description" id="panel-story-info-description"><h3>Description :</h3>Jinwoo is weak.</div>`

	mboxChaptersJSON = `{"success":true,"data":{"chapters":[
	  {"chapter_name":"Chapter 201","chapter_slug":"chapter-201","chapter_num":201,"updated_at":"2024-05-01T08:00:00.000Z"},
	  {"chapter_name":"Chapter 200.5","chapter_slug":"chapter-200-5","chapter_num":200.5,"updated_at":null},
	  {"chapter_name":"Broken","chapter_slug":null,"chapter_num":null,"updated_at":null}]}}`

	mboxChapterHTML = `<html><body><div class="container-chapter-reader"><img src="/fallback.jpg"></div>
<script>var cdns = ["https:\/\/img-r1.example\/"]; var backupImage = ["https:\/\/img-b1.example"];
var chapterImages = ["manga\/solo\/1.webp", "\/manga\/solo\/2.webp"];</script></body></html>`
)

func newMangaBox(t *testing.T, details string) *mboxSite {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/manga-list/hot-manga", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			t.Errorf("page %q", r.URL.Query().Get("page"))
		}
		_, _ = w.Write([]byte(mboxHotHTML))
	})
	mux.HandleFunc("/search/story/solo_leveling", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" || r.Header.Get("Referer") == "" {
			t.Errorf("the site wants a referer and no origin: %v", r.Header)
		}
		_, _ = w.Write([]byte(mboxSearchHTML))
	})
	mux.HandleFunc("/manga/solo-leveling", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(details)) })
	mux.HandleFunc("/api/manga/solo-leveling/chapters", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "-1" {
			t.Errorf("limit %q", r.URL.Query().Get("limit"))
		}
		_, _ = w.Write([]byte(mboxChaptersJSON))
	})
	mux.HandleFunc("/manga/solo-leveling/chapter-201", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(mboxChapterHTML))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &mboxSite{c: sourcekit.NewClient(srv.Client()), id: "test", name: "Mangakakalot", base: srv.URL, oldIDSlugs: true}
}

// TestMangaBox reads a series the way the library does: find it, read its
// details, list chapters, then a chapter's pages.
func TestMangaBox(t *testing.T) {
	m := newMangaBox(t, mboxKakalotHTML)
	ctx := context.Background()

	pop, err := m.Popular(ctx, 1)
	if err != nil || len(pop.Mangas) != 1 || !pop.HasNext {
		t.Fatalf("popular: %v %+v", err, pop)
	}
	if got := pop.Mangas[0]; got.URL != "/manga/solo-leveling" || got.Title != "Solo Leveling" || got.CoverURL != "https://cdn.example/solo.jpg" {
		t.Fatalf("popular result %+v", got)
	}

	res, err := m.Search(ctx, "Solo Leveling!", 1)
	if err != nil || len(res.Mangas) != 1 || res.HasNext {
		t.Fatalf("search: %v %+v", err, res)
	}
	got := res.Mangas[0]
	if got.URL != "/manga/solo-leveling" || got.ID != "solo-leveling" || got.CoverURL != m.base+"/covers/solo.jpg" {
		t.Fatalf("result %+v", got)
	}

	d, err := m.Details(ctx, sourcekit.Ref{URL: got.URL})
	if err != nil || d.Title != "Solo Leveling" || d.Author != "Chugong, DUBU" || d.Status != sourcekit.StatusCompleted ||
		strings.Join(d.Genres, "|") != "Action|Fantasy" || d.CoverURL != "https://cdn.example/solo.jpg" {
		t.Fatalf("details: %v %+v", err, d)
	}
	if d.Description != "A weak hunter.\n\nAlternative Name: Na Honjaman Level Up" {
		t.Fatalf("description %q", d.Description)
	}

	chs, err := m.Chapters(ctx, sourcekit.Ref{URL: got.URL})
	if err != nil || len(chs) != 2 {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
	if chs[0].URL != "/manga/solo-leveling/chapter-201" || chs[0].Number != 201 || chs[0].UploadedAt == nil ||
		chs[1].Number != 200.5 || chs[1].UploadedAt != nil || !strings.HasPrefix(chs[0].Scanlator, "127.0.0.1") {
		t.Fatalf("chapters %+v", chs)
	}

	pages, err := m.Pages(ctx, sourcekit.PageRef{URL: chs[0].URL})
	if err != nil || len(pages) != 2 {
		t.Fatalf("pages: %v %+v", err, pages)
	}
	if pages[0].URL != "https://img-r1.example/manga/solo/1.webp" || pages[1].URL != "https://img-r1.example/manga/solo/2.webp" ||
		pages[0].Headers["Referer"] == "" {
		t.Fatalf("pages %+v", pages)
	}
}

// TestMangaBoxNelo: the other layout of the network's manga page.
func TestMangaBoxNelo(t *testing.T) {
	m := newMangaBox(t, mboxNeloHTML)
	d, err := m.Details(context.Background(), sourcekit.Ref{URL: "https://www.natomanga.com/manga/solo-leveling"})
	if err != nil || d.Title != "Solo Leveling" || d.Author != "Chugong" || d.Status != sourcekit.StatusOngoing ||
		strings.Join(d.Genres, "|") != "Action|Fantasy" || d.CoverURL != m.base+"/covers/solo.jpg" {
		t.Fatalf("details: %v %+v", err, d)
	}
	if d.Description != "Jinwoo is weak.\n\nAlternative Name: Only I Level Up" {
		t.Fatalf("description %q", d.Description)
	}
}

// TestMangaBoxSlugs: links from Mangakakalot's previous engine end in an id,
// so the slug is made from the title; Manganato's old domains need migrating.
func TestMangaBoxSlugs(t *testing.T) {
	kakalot := &mboxSite{name: "Mangakakalot", oldIDSlugs: true}
	for ref, want := range map[sourcekit.Ref]string{
		{URL: "/manga/solo-leveling"}: "solo-leveling",
		{URL: "https://mangakakalot.com/manga/ht123456", Title: "Solo Leveling: Ragnarok!"}: "solo-leveling-ragnarok",
		{URL: "/manga/ht123456"}: "ht123456",
	} {
		if got, err := kakalot.slug(ref); err != nil || got != want {
			t.Errorf("slug(%+v) = %q %v, want %q", ref, got, err, want)
		}
	}
	nato := &mboxSite{name: "Manganato", legacyDomains: []string{"https://chapmanganato.to/", "https://manganato.com/"}}
	if _, err := nato.slug(sourcekit.Ref{URL: "https://manganato.com/manga-ab123"}); err == nil {
		t.Error("an entry on an old domain should ask for a migration")
	}
	if got, err := nato.slug(sourcekit.Ref{URL: "/manga/ab123"}); err != nil || got != "ab123" {
		t.Errorf("Manganato keeps id slugs: %q %v", got, err)
	}
}

func TestManganatoUsesTheUnchallengedMirror(t *testing.T) {
	var nato *mboxSite
	for i := range mboxSites {
		if mboxSites[i].name == "Manganato" {
			nato = &mboxSites[i]
			break
		}
	}
	if nato == nil || nato.base != "https://www.manganato.gg" {
		t.Fatalf("Manganato base = %+v", nato)
	}
	// Existing entries can contain an absolute URL from the previous mirror;
	// the stored slug is sufficient to rebase them without a migration.
	if got, err := nato.slug(sourcekit.Ref{URL: "https://www.natomanga.com/manga/dandadan"}); err != nil || got != "dandadan" {
		t.Fatalf("previous mirror slug = %q, %v", got, err)
	}
}

// TestMangaBoxNormalize is the site's own search normalization.
func TestMangaBoxNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"Solo Leveling!":     "solo_leveling",
		"Re:Zero - Starting": "re_zero_starting",
		"Đại Quản Gia Là Ma": "dai_quan_gia_la_ma",
		"  ...  ":            "",
		"$money$":            "$money$",
	} {
		if got := mboxNormalize(in); got != want {
			t.Errorf("mboxNormalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestMangaBoxImagesFromTags: without a script listing, the reader's <img>
// tags are the pages.
func TestMangaBoxImagesFromTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<div class="container-chapter-reader"><img src="/p/1.jpg"><img src="https://x.example/2.jpg"></div>
		<script>var cdns = ["https://img.example"];</script>`))
	}))
	defer srv.Close()
	m := &mboxSite{c: sourcekit.NewClient(srv.Client()), base: srv.URL}
	pages, err := m.Pages(context.Background(), sourcekit.PageRef{URL: "/manga/x/chapter-1"})
	if err != nil || len(pages) != 2 || pages[0].URL != srv.URL+"/p/1.jpg" || pages[1].URL != "https://x.example/2.jpg" {
		t.Fatalf("pages: %v %+v", err, pages)
	}
}
