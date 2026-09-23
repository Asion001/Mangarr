package sites

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// mdotFlatten encodes a value the way React Router's ".data" routes do, so
// fixtures can be written as plain values: element 0 is the root, arrays
// hold item indexes, objects map "_<key index>" to value indexes, and a
// missing value is a negative index.
func mdotFlatten(t *testing.T, v any) []byte {
	t.Helper()
	var flat []any
	var add func(v any) int
	add = func(v any) int {
		i := len(flat)
		flat = append(flat, nil)
		switch x := v.(type) {
		case map[string]any:
			obj := map[string]int{}
			for k, val := range x {
				ki := len(flat)
				flat = append(flat, k)
				if val == nil {
					obj["_"+strconv.Itoa(ki)] = -5
					continue
				}
				obj["_"+strconv.Itoa(ki)] = add(val)
			}
			flat[i] = obj
		case []any:
			idx := make([]int, 0, len(x))
			for _, item := range x {
				idx = append(idx, add(item))
			}
			flat[i] = idx
		default:
			flat[i] = x
		}
		return i
	}
	add(v)
	b, err := json.Marshal(flat)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newMangaDot(t *testing.T) *mangadot {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("search") != "berserk" || q.Get("limit") != "56" || q.Get("sortOrder") != "desc" ||
			q.Get("content_rating") != "-erotica,-pornographic" {
			t.Errorf("search query %v", q)
		}
		_, _ = w.Write([]byte(`{"results":[{"manga_id":12,"title":"Berserk","photo":"/covers/12.jpg","is_blurworthy":0},
			{"manga_id":13,"title":"Berserk of Gluttony","photo":"https://img.example/13.jpg"}],
			"pagination":{"current_page":1,"total_pages":3}}`))
	})
	mux.HandleFunc("/view-all/most-tracked.data", func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query(); q.Get("adult") != "0" || q.Get("_routes") != "pages/ViewAllPage" {
			t.Errorf("view-all query %v", q)
		}
		_, _ = w.Write(mdotFlatten(t, map[string]any{"pages/ViewAllPage": map[string]any{"data": map[string]any{
			"data": map[string]any{
				"manga_list": []any{map[string]any{"id": 12, "title": "Berserk", "photo": "/covers/12.jpg"}},
				"pagination": map[string]any{"page": 2, "lastPage": 2},
			},
			"allGenres": []any{"Action"},
		}}}))
	})
	mux.HandleFunc("/manga/12.data", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("_routes") != "pages/MangaDetailPage" {
			t.Errorf("details route %q", r.URL.Query().Get("_routes"))
		}
		_, _ = w.Write(mdotFlatten(t, map[string]any{"pages/MangaDetailPage": map[string]any{"data": map[string]any{
			"mangaData": map[string]any{
				"manga": map[string]any{"id": 12, "title": "Berserk", "photo": "/covers/12.jpg",
					"genres": []any{"Action", "Seinen"}, "status": "Ongoing", "hiatus": "Yes",
					"country_of_origin": "JP", "description": "Guts.\r\n\r\n\r\n\r\nA mercenary.",
					"authors": `["Kentaro Miura"]`, "artists": `["Studio Gaga"]`, "source_url": nil,
					"tags": []any{map[string]any{"category": "Theme", "tags": []any{
						map[string]any{"name": "Revenge"}, map[string]any{"name": "Demons"}}}}},
				"total_volumes": nil,
			},
		}}}))
	})
	mux.HandleFunc("/api/manga/12/chapters/list", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("lang") != "en" {
			t.Errorf("chapters for %q", r.URL.Query().Get("lang"))
		}
		// oldest first, one in another language, one a user upload
		_, _ = w.Write([]byte(`[
			{"id":554,"chapter_number":1,"chapter_title":"The Black Swordsman","language":"en","source":"mangadex","group_name":"Evil Genius","date_added":"2024-01-02 03:04:05"},
			{"id":999,"chapter_number":1,"chapter_title":"","language":"fr","source":"mangadex"},
			{"id":555,"chapter_number":1.5,"chapter_title":"Chapter 1.5 Extra","scanlator_name":"Fans","date_added":"2024-02-03T04:05:06Z"}]`))
	})
	mux.HandleFunc("/api/uploads/555/images", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") == "" {
			t.Error("the site expects a referer")
		}
		_, _ = w.Write([]byte(`{"manga":{"id":12},"images":[{"url":"/img/1.webp"},{"url":"https://cdn.example/2.webp"},{"url":"junk"}]}`))
	})
	mux.HandleFunc("/api/chapters/554/images", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"manga":{"id":12},"images":[{"url":"/img/a.webp"}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &mangadot{c: sourcekit.NewClient(srv.Client()), base: srv.URL, lang: "en", adult: "none", rating: "suggestive"}
}

// TestMangaDot walks a series the way the library does: find it, read its
// details, list chapters, then a chapter's pages.
func TestMangaDot(t *testing.T) {
	m := newMangaDot(t)
	ctx := context.Background()

	res, err := m.Search(ctx, "berserk", 1)
	if err != nil || len(res.Mangas) != 2 || !res.HasNext {
		t.Fatalf("search: %v %+v", err, res)
	}
	if got := res.Mangas[0]; got.URL != "12" || got.Title != "Berserk" || got.CoverURL != m.base+"/covers/12.jpg" {
		t.Fatalf("result %+v", got)
	}

	pop, err := m.Popular(ctx, 1)
	if err != nil || len(pop.Mangas) != 1 || pop.Mangas[0].URL != "12" || pop.HasNext {
		t.Fatalf("popular: %v %+v", err, pop)
	}

	d, err := m.Details(ctx, sourcekit.Ref{URL: "12"})
	if err != nil || d.Title != "Berserk" || d.Author != "Kentaro Miura" || d.Artist != "Studio Gaga" ||
		d.Status != sourcekit.StatusHiatus || d.Description != "Guts.\n\nA mercenary." {
		t.Fatalf("details: %v %+v", err, d)
	}
	want := []string{"Manga", "Action", "Seinen", "Demons", "Revenge"}
	if len(d.Genres) != len(want) {
		t.Fatalf("genres %v", d.Genres)
	}
	for i := range want {
		if d.Genres[i] != want[i] {
			t.Fatalf("genres %v, want %v", d.Genres, want)
		}
	}

	chs, err := m.Chapters(ctx, sourcekit.Ref{URL: "12"})
	if err != nil || len(chs) != 2 {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
	// newest first, named like the extension names them, the url kept as
	// Keiyoushi stores it
	if chs[0].Name != "Chapter 1.5 Extra" || chs[0].Number != 1.5 || chs[0].Scanlator != "Fans" || chs[0].UploadedAt == nil ||
		chs[0].URL != `{"id":"555","source":"user","isVolume":false}` {
		t.Fatalf("first chapter %+v", chs[0])
	}
	if chs[1].Name != "Chapter 1: The Black Swordsman" || chs[1].Scanlator != "Evil Genius" || chs[1].UploadedAt == nil ||
		chs[1].WebURL != m.base+"/chapter/554" {
		t.Fatalf("second chapter %+v", chs[1])
	}

	pages, err := m.Pages(ctx, sourcekit.PageRef{URL: chs[0].URL})
	if err != nil || len(pages) != 2 {
		t.Fatalf("pages: %v %+v", err, pages)
	}
	if pages[0].URL != m.base+"/img/1.webp" || pages[1].URL != "https://cdn.example/2.webp" ||
		pages[0].Headers["Referer"] != m.base+"/chapter/555?source=user" {
		t.Fatalf("pages %+v", pages)
	}
	// a link to the chapter's page works too
	if pages, err := m.Pages(ctx, sourcekit.PageRef{URL: m.base + "/chapter/554"}); err != nil || len(pages) != 1 {
		t.Fatalf("pages from a link: %v %+v", err, pages)
	}
}

// TestMangaDotDecodeFlat pins the data format against a hand-written answer:
// keys and values are indexes, values can be shared, and negative indexes
// are missing values.
func TestMangaDotDecodeFlat(t *testing.T) {
	raw := `[{"_1":2,"_3":4},"a",[5,5,-5],"b",{"_1":6},"x",7]`
	var flat []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &flat); err != nil {
		t.Fatal(err)
	}
	v, err := mdotDecodeFlat(flat)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(v)
	if string(got) != `{"a":["x","x",null],"b":{"a":7}}` {
		t.Fatalf("decoded %s", got)
	}
	if _, err := mdotDecodeFlat(nil); err == nil {
		t.Fatal("empty data should be refused")
	}
}

// TestMangaDotLanguages: the languages Keiyoushi pinned ids for keep them,
// the others get derived ids, and all are distinct catalogs.
func TestMangaDotLanguages(t *testing.T) {
	infos := map[string]sourcekit.Info{}
	for _, s := range sourcekit.Build(sourcekit.Deps{Client: sourcekit.NewClient(nil)}) {
		if i := s.Info(); i.Name == "MangaDot" {
			infos[i.ID] = i
		}
	}
	if len(infos) != len(mdotPinnedIDs)+len(mdotNewLangs) {
		t.Fatalf("%d MangaDot catalogs", len(infos))
	}
	if en := infos["5900936305360403385"]; en.Lang != "en" {
		t.Fatalf("English catalog %+v", en)
	}
	if zu := infos[sourcekit.KeiyoushiID("MangaDot", "zu", 1)]; zu.Lang != "zu" {
		t.Fatalf("Zulu catalog %+v", zu)
	}
	if got := (&mangadot{lang: "es-419"}).queryLang(); got != "es-la" {
		t.Fatalf("es-419 asks the site for %q", got)
	}
}
