package sites

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// The answers below have the shapes the extension reads: the home page's
// popular covers, the search page's chapter list, and the JSON every entry
// has at its address plus ".json". "drcl_midnight_children" is in the
// extension's cover map; "some_doujin" is not.
const (
	dynHomeHTML = `
<div class="span8"><h4>Most Popular of Past 7 Days</h4>
  <ul class="thumbnails cover-list">
    <li><a class="thumbnail" href="/chapters/drcl_midnight_children_ch01"><img src="/x.jpg"></a></li>
    <li><a class="thumbnail" href="/chapters/a_lone_oneshot"><img src="/y.jpg"></a></li>
  </ul></div>
<div class="span4"><h4>Newest</h4><ul class="cover-list"><li><a class="thumbnail" href="/chapters/not_popular"></a></li></ul></div>`

	dynSearchHTML = `
<dl class="chapter-list">
  <dd><a class="name" href="/series/drcl_midnight_children">DRCL midnight children</a> <small>Series</small></dd>
  <dd><a class="name" href="/chapters/drcl_midnight_children_ch02">DRCL midnight children ch02</a>
      <span class="doujin_tags"><a href="/doujins/some_doujin">Some Doujin</a></span></dd>
  <dd><a class="name" href="/chapters/a_lone_oneshot">A Lone Oneshot <small>by X</small></a></dd>
  <dd><a class="name" href="/tags/yuri">Yuri</a></dd>
</dl>
<div class="pagination"><ul><li class="active"><a>1</a></li><li><a rel="next" href="/search?page=2">2</a></li></ul></div>`

	dynAddedJSON = `{"chapters":[
	  {"title":"Chapter 5","permalink":"drcl_midnight_children_ch05","tags":[
	    {"type":"Series","name":"DRCL midnight children","permalink":"drcl_midnight_children"},
	    {"type":"Author","name":"Shinichi Sakamoto","permalink":"sakamoto"}]},
	  {"title":"A Lone Oneshot","permalink":"a_lone_oneshot","tags":[{"type":"Doujin","name":"Some Doujin","permalink":"some_doujin"}]}],
	  "current_page":1,"total_pages":5}`

	dynSeriesJSON = `{"name":"DRCL midnight children","type":"Series","permalink":"drcl_midnight_children",
	  "tags":[{"type":"Author","name":"Shinichi Sakamoto","permalink":"sakamoto"},
	          {"type":"General","name":"Vampires","permalink":"vampires"},
	          {"type":"Status","name":"Ongoing","permalink":"ongoing"}],
	  "cover":"/system/tag_contents_covers/000/015/786/medium/58529.jpg",
	  "description":"<p>Mina Murray \\u00e9 <a href=\"/x\">(source)</a> story</p>","aliases":["DRCL"],
	  "taggings":[{"header":"Volume 1"},
	    {"title":"Chapter 1","permalink":"drcl_midnight_children_ch01","released_on":"2023-01-01",
	     "tags":[{"type":"Scanlator","name":"Team A","permalink":"team_a"}]},
	    {"title":"Chapter 2","permalink":"drcl_midnight_children_ch02","released_on":"2023-02-01",
	     "tags":[{"type":"General","name":"Blood","permalink":"blood"},{"type":"Pairing","name":"Mina x Lucy","permalink":"ml"}]}],
	  "total_pages":3}`

	dynSeriesPage2JSON = `{"name":"DRCL midnight children","type":"Series","permalink":"drcl_midnight_children","tags":[],
	  "aliases":[],"taggings":[{"title":"Chapter 3","permalink":"drcl_midnight_children_ch03","released_on":"2023-03-01","tags":[]}],
	  "total_pages":3}`

	dynDoujinJSON = `{"name":"Some Doujin","type":"Doujin","permalink":"some_doujin","tags":[],
	  "cover":"/system/tag_contents_covers/000/999/111/medium/x.jpg","description":null,"aliases":[],
	  "taggings":[{"title":"Oneshot A","permalink":"oneshot_a","released_on":"2021-01-01",
	     "tags":[{"type":"Author","name":"X","permalink":"x"},{"type":"Author","name":"Y","permalink":"y"}]},
	    {"title":"Oneshot B","permalink":"oneshot_b","released_on":"2021-02-01","tags":[]}],"total_pages":1}`

	dynLoneJSON = `{"title":"A Lone Oneshot","permalink":"a_lone_oneshot","released_on":"2022-05-05",
	  "tags":[{"type":"Author","name":"X","permalink":"x"},{"type":"Scanlator","name":"S","permalink":"s"},
	          {"type":"General","name":"Comedy","permalink":"comedy"}],
	  "pages":[{"url":"/system/releases/000/001/a.jpg"},{"url":"/system/releases/000/001/b.jpg"}]}`
)

type dynServer struct {
	mu    sync.Mutex
	heads []string
}

func newDynasty(t *testing.T) (*dynasty, *dynServer) {
	t.Helper()
	ds := &dynServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept"), "text/html") {
			t.Errorf("home accept %q", r.Header.Get("Accept"))
		}
		_, _ = w.Write([]byte(dynHomeHTML))
	})
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("q") != "drcl" || !q.Has("sort") || q.Get("sort") != "" || len(q["classes[]"]) != 5 {
			t.Errorf("search query %v", q)
		}
		_, _ = w.Write([]byte(dynSearchHTML))
	})
	mux.HandleFunc("/chapters/added.json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			t.Errorf("added page %q", r.URL.Query().Get("page"))
		}
		_, _ = w.Write([]byte(dynAddedJSON))
	})
	mux.HandleFunc("/series/drcl_midnight_children.json", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "":
			_, _ = w.Write([]byte(dynSeriesJSON))
		case "2":
			_, _ = w.Write([]byte(dynSeriesPage2JSON))
		default:
			t.Errorf("only the first two pages are read: page %s", r.URL.Query().Get("page"))
		}
	})
	mux.HandleFunc("/doujins/some_doujin.json", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(dynDoujinJSON)) })
	mux.HandleFunc("/chapters/a_lone_oneshot.json", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(dynLoneJSON)) })
	mux.HandleFunc("/system/tag_contents_covers/", func(w http.ResponseWriter, r *http.Request) {
		ds.mu.Lock()
		ds.heads = append(ds.heads, r.Method+" "+r.URL.Path)
		ds.mu.Unlock()
		if !strings.HasSuffix(r.URL.Path, "/original/x.png") {
			w.WriteHeader(http.StatusNotFound)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &dynasty{c: sourcekit.NewClient(srv.Client()), base: srv.URL, fetchLimit: 2}, ds
}

func dynURLs(res sourcekit.Results) string {
	var out []string
	for _, m := range res.Mangas {
		out = append(out, m.URL)
	}
	return strings.Join(out, " ")
}

// TestDynasty reads a series the way the library does: find it, read its
// details, list chapters, then a chapter's pages.
func TestDynasty(t *testing.T) {
	s, ds := newDynasty(t)
	ctx := context.Background()

	// chapters found are listed as their series
	res, err := s.Search(ctx, "drcl", 1)
	if err != nil || !res.HasNext || dynURLs(res) != "/series/drcl_midnight_children /doujins/some_doujin /chapters/a_lone_oneshot" {
		t.Fatalf("search: %v %+v", err, res)
	}
	got := res.Mangas[0]
	if got.Title != "DRCL midnight children" ||
		got.CoverURL != s.base+"/system/tag_contents_covers/000/015/786/original/58529.jpg" {
		t.Fatalf("result %+v", got)
	}
	if res.Mangas[2].Title != "A Lone Oneshot" || res.Mangas[2].CoverURL != "" {
		t.Fatalf("lone chapter %+v", res.Mangas[2])
	}

	d, err := s.Details(ctx, sourcekit.Ref{URL: got.URL})
	if err != nil || d.Title != "DRCL midnight children" || d.Author != "Shinichi Sakamoto" || d.Artist != d.Author ||
		d.Status != sourcekit.StatusOngoing || strings.Join(d.Genres, "|") != "Vampires|Blood" {
		t.Fatalf("details: %v %+v", err, d)
	}
	// the site's cover is the cached one at medium size: the cached one wins
	// without asking the site for an original
	if d.CoverURL != got.CoverURL || len(ds.heads) != 0 {
		t.Fatalf("cover %q, lookups %v", d.CoverURL, ds.heads)
	}
	for _, want := range []string{"IMPORTANT: Only the first 2 pages", "Mina Murray é  story\n\nType: Series",
		"Status:\n• Ongoing", "Pairing:\n• Mina x Lucy", "Aliases:\n• DRCL"} {
		if !strings.Contains(d.Description, want) {
			t.Fatalf("description %q lacks %q", d.Description, want)
		}
	}
	if strings.Contains(d.Description, "(source)") {
		t.Fatalf("links are dropped from the description: %q", d.Description)
	}

	chs, err := s.Chapters(ctx, sourcekit.Ref{URL: got.URL})
	if err != nil || len(chs) != 3 {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
	if chs[0].Name != "Volume 1 Chapter 3" || chs[0].Number != 3 || chs[2].Name != "Volume 1 Chapter 1" ||
		chs[2].Scanlator != "Team A" || chs[2].URL != "/chapters/drcl_midnight_children_ch01" || chs[2].UploadedAt == nil {
		t.Fatalf("chapters are newest first: %+v", chs)
	}

	pages, err := s.Pages(ctx, sourcekit.PageRef{URL: "/chapters/a_lone_oneshot"})
	if err != nil || len(pages) != 2 || pages[0].URL != s.base+"/system/releases/000/001/a.jpg" || pages[1].Index != 1 {
		t.Fatalf("pages: %v %+v", err, pages)
	}
}

// TestDynastyBrowse: popular is the home page's week, latest the chapters
// added lately; both name series rather than their chapters.
func TestDynastyBrowse(t *testing.T) {
	s, _ := newDynasty(t)
	ctx := context.Background()
	pop, err := s.Popular(ctx, 1)
	if err != nil || !pop.HasNext || dynURLs(pop) != "/series/drcl_midnight_children /chapters/a_lone_oneshot" {
		t.Fatalf("popular: %v %+v", err, pop)
	}
	if pop.Mangas[0].Title != "Drcl Midnight Children" {
		t.Fatalf("a title from a permalink %q", pop.Mangas[0].Title)
	}
	latest, err := s.Latest(ctx, 1)
	if err != nil || !latest.HasNext ||
		dynURLs(latest) != "/series/drcl_midnight_children /doujins/some_doujin /chapters/a_lone_oneshot" {
		t.Fatalf("latest: %v %+v", err, latest)
	}
}

// TestDynastyOtherEntries: a doujin (credited chapters, site order, its
// original-size cover looked up) and a lone chapter (its own only chapter).
func TestDynastyOtherEntries(t *testing.T) {
	s, ds := newDynasty(t)
	ctx := context.Background()

	d, err := s.Details(ctx, sourcekit.Ref{URL: "/doujins/some_doujin"})
	if err != nil || d.CoverURL != s.base+"/system/tag_contents_covers/000/999/111/original/x.png" || len(ds.heads) != 3 ||
		!strings.HasPrefix(ds.heads[0], "HEAD ") {
		t.Fatalf("doujin: %v %+v, lookups %v", err, d, ds.heads)
	}
	chs, err := s.Chapters(ctx, sourcekit.Ref{URL: "/doujins/some_doujin"})
	if err != nil || len(chs) != 2 || chs[0].Name != "Oneshot A by X and Y" || chs[1].Name != "Oneshot B" {
		t.Fatalf("doujin chapters: %v %+v", err, chs)
	}

	d, err = s.Details(ctx, sourcekit.Ref{URL: "/chapters/a_lone_oneshot"})
	if err != nil || d.Title != "A Lone Oneshot" || d.Status != sourcekit.StatusCompleted || d.Author != "X" ||
		d.CoverURL != s.base+"/system/releases/000/001/a.jpg" || !strings.HasSuffix(d.Description, "Released: 2022-05-05") {
		t.Fatalf("lone chapter: %v %+v", err, d)
	}
	chs, err = s.Chapters(ctx, sourcekit.Ref{URL: "/chapters/a_lone_oneshot"})
	if err != nil || len(chs) != 1 || chs[0].URL != "/chapters/a_lone_oneshot" || chs[0].Scanlator != "S" {
		t.Fatalf("lone chapter chapters: %v %+v", err, chs)
	}

	if _, err := s.Details(ctx, sourcekit.Ref{URL: "https://dynasty-scans.com/series/a/b"}); err == nil {
		t.Fatal("a link that isn't an entry should be refused")
	}
}

// TestDynastyResolve: a chapter's permalink names its series when it has one.
func TestDynastyResolve(t *testing.T) {
	for in, want := range map[string]string{
		"drcl_midnight_children_ch01":    "series/drcl_midnight_children",
		"citrus_ch50_5":                  "series/citrus",
		"bloom_into_you_volume_3_extras": "series/bloom_into_you",
		"a_lone_oneshot":                 "chapters/a_lone_oneshot",
	} {
		if dir, p := dynResolve("chapters", in); dir+"/"+p != want {
			t.Errorf("dynResolve(%q) = %s/%s, want %s", in, dir, p, want)
		}
	}
	if dir, p := dynResolve("doujins", "x_ch01"); dir != "doujins" || p != "x_ch01" {
		t.Errorf("only chapters resolve: %s/%s", dir, p)
	}
}

// TestDynastyOptions: the chapter list page limit.
func TestDynastyOptions(t *testing.T) {
	s := &dynasty{fetchLimit: 2}
	if err := s.SetOption("chapterFetchLimit", "all"); err != nil || s.fetchLimit != 0 || s.Options()[0].Value != "all" {
		t.Fatalf("all: %v %d", err, s.fetchLimit)
	}
	if err := s.SetOption("chapterFetchLimit", "10"); err != nil || s.fetchLimit != 10 {
		t.Fatalf("10: %v %d", err, s.fetchLimit)
	}
	if s.SetOption("chapterFetchLimit", "lots") == nil || s.SetOption("other", "1") == nil {
		t.Fatal("bad options should be refused")
	}
}
