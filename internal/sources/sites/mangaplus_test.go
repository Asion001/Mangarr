package sites

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// mpEnc writes protobuf the way the API does, so the fixtures below carry the
// field numbers the extension's Dto.kt declares.
type mpEnc []byte

func (e mpEnc) uvarint(v uint64) mpEnc {
	for v >= 0x80 {
		e = append(e, byte(v)|0x80)
		v >>= 7
	}
	return append(e, byte(v))
}

func (e mpEnc) num(f int, v int64) mpEnc { return e.uvarint(uint64(f << 3)).uvarint(uint64(v)) }

func (e mpEnc) str(f int, s string) mpEnc {
	return append(e.uvarint(uint64(f<<3|2)).uvarint(uint64(len(s))), s...)
}

func (e mpEnc) msg(f int, m mpEnc) mpEnc { return e.str(f, string(m)) }

// mpTitleEnc is a Title: 1 id, 2 name, 3 author, 4 portrait, 7 language
// (left out for English, as proto3 leaves out a zero).
func mpTitleEnc(id int, name, author string, lang int) mpEnc {
	t := mpEnc{}.num(1, int64(id)).str(2, name).str(3, author).str(4, "https://img.example/"+name+".jpg")
	if lang != 0 {
		t = t.num(7, int64(lang))
	}
	return t
}

// mpSuccess wraps a view in a response's success result.
func mpSuccess(field int, view mpEnc) []byte { return mpEnc{}.msg(1, mpEnc{}.msg(field, view)) }

// mpChapterEnc is a Chapter: 2 id, 3 name, 4 subtitle (absent: expired), 6 start.
func mpChapterEnc(id int, name string, sub *string, start int64) mpEnc {
	c := mpEnc{}.num(2, int64(id)).str(3, name)
	if sub != nil {
		c = c.str(4, *sub)
	}
	return c.num(6, start)
}

// mpFixtures are the API's answers, by path.
func mpFixtures() map[string][]byte {
	sub := func(s string) *string { return &s }
	onePiece := mpTitleEnc(100020, "One Piece", "Eiichiro Oda", 0)
	updated := func(title mpEnc, at int64) mpEnc { return mpEnc{}.msg(3, mpEnc{}.msg(1, title)).num(6, at) }
	return map[string][]byte{
		// allV2 (25): groups (1) of titles (2), every language mixed in
		"/api/title_list/allV2": mpSuccess(25, mpEnc{}.
			msg(1, mpEnc{}.msg(2, onePiece).msg(2, mpTitleEnc(100021, "One Piece", "Eiichiro Oda", 1))).
			msg(1, mpEnc{}.msg(2, mpTitleEnc(100191, "Sakamoto Days", "Yuto Suzuki / Studio", 0)).msg(2, onePiece).
				msg(2, mpTitleEnc(0, "Broken One", "", 0)))),
		// rankingV2 (37): ranked (3) titles (2)
		"/api/title_list/rankingV2": mpSuccess(37, mpEnc{}.
			msg(3, mpEnc{}.msg(2, onePiece)).msg(3, mpEnc{}.msg(2, mpTitleEnc(5, "Otro", "", 1)))),
		// web_homeV4 (38): groups (2) of updated titles (2), and the featured one (7)
		"/api/web/web_homeV4": mpSuccess(38, mpEnc{}.
			msg(2, mpEnc{}.msg(2, updated(onePiece, 100)).msg(2, updated(mpTitleEnc(7, "Older", "", 0), 50))).
			msg(7, mpEnc{}.msg(2, updated(mpTitleEnc(8, "Newest", "", 0), 200)))),
		// title_detailV3 (8): the title, its texts and chapter groups (28)
		"/api/title_detailV3?100020": mpSuccess(8, mpEnc{}.msg(1, onePiece).
			str(3, "Gol D. Roger was known as the Pirate King.").str(7, "This series is updated every Sunday.").
			msg(28, mpEnc{}.
				msg(2, mpChapterEnc(1000001, "#001", sub("Romance Dawn"), 1579219200)).
				msg(2, mpChapterEnc(1000002, "#002", nil, 1579219200)).
				msg(4, mpChapterEnc(1000486, "#1100", sub("Thank You, Bonney"), 1783633137)).
				msg(4, mpChapterEnc(1000487, "Ex", sub("Special"), 1783700000))).
			msg(31, mpEnc{}.str(1, "Action").str(2, "action")).msg(31, mpEnc{}.str(1, "Adventure").str(2, "adventure"))),
		// a title in Spanish: not this (English) catalog's
		"/api/title_detailV3?100021": mpSuccess(8, mpEnc{}.msg(1, mpTitleEnc(100021, "One Piece", "", 1))),
		// a one-shot, finished
		"/api/title_detailV3?100300": mpSuccess(8, mpEnc{}.msg(1, mpTitleEnc(100300, "Short", "", 0)).
			str(8, "This title has been completed.").msg(31, mpEnc{}.str(1, "One-shot").str(2, "one-shot"))),
		// a removed title: the error (2) with its English popup (2)
		"/api/title_detailV3?999": mpEnc{}.msg(2, mpEnc{}.msg(2, mpEnc{}.str(1, "Not Found").str(2, "This title was removed."))),
		// manga_viewer_v3 (10): pages (1) holding a manga page (1) or not,
		// and the view token (19)
		"/api/manga_viewer_v3": mpSuccess(10, mpEnc{}.
			msg(1, mpEnc{}.msg(1, mpEnc{}.str(1, "https://cdn.example/p1.jpg?key=x").str(5, "0a0b0c"))).
			msg(1, mpEnc{}.msg(1, mpEnc{}.str(1, "https://cdn.example/p2.jpg").str(5, "ff"))).
			msg(1, mpEnc{}.msg(3, mpEnc{}.str(1, "last page card"))).
			num(9, 100020).str(19, "view-token")),
	}
}

func newMangaPlus(t *testing.T) *mangaplus {
	t.Helper()
	fixtures := mpFixtures()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("SESSION-TOKEN") != "the-session" {
			t.Errorf("%s without the session token", r.URL.Path)
		}
		q := r.URL.Query()
		key := r.URL.Path
		switch key {
		case "/api/title_list/rankingV2":
			if q.Get("lang") != "eng" || q.Get("clang") != "eng" || q.Get("type") != "hottest" {
				t.Errorf("ranking %v", q)
			}
		case "/api/title_detailV3":
			if q.Get("clang") != "eng" {
				t.Errorf("detail %v", q)
			}
			key += "?" + q.Get("title_id")
		case "/api/manga_viewer_v3":
			if q.Get("chapter_id") != "1000486" || q.Get("split") != "yes" || q.Get("img_quality") != "super_high" ||
				q.Get("clang") != "eng" {
				t.Errorf("viewer %v", q)
			}
		}
		body, ok := fixtures[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return &mangaplus{c: sourcekit.NewClient(srv.Client()), site: "https://mangaplus.shueisha.co.jp", api: srv.URL + "/api",
		code: "en", session: "the-session", quality: "super_high", split: true}
}

// TestMangaPlus walks a whole library flow: find a title, read its details,
// list its chapters and get a chapter's (encrypted) pages.
func TestMangaPlus(t *testing.T) {
	m := newMangaPlus(t)
	ctx := context.Background()

	// every title of every language comes back; only English ones, once
	// each and with a real id, are this catalog's
	res, err := m.Search(ctx, "one", 1)
	if err != nil || len(res.Mangas) != 1 {
		t.Fatalf("search: %v %+v", err, res)
	}
	if got := res.Mangas[0]; got.URL != "#/titles/100020" || got.Title != "One Piece" || got.CoverURL == "" {
		t.Fatalf("result %+v", got)
	}
	// authors match too
	if res, err := m.Search(ctx, "suzuki", 1); err != nil || len(res.Mangas) != 1 || res.Mangas[0].Title != "Sakamoto Days" {
		t.Fatalf("search by author: %v %+v", err, res)
	}
	if res, _ := m.Search(ctx, "one", 2); len(res.Mangas) != 0 || res.HasNext {
		t.Fatalf("the API has one page: %+v", res)
	}

	pop, err := m.Popular(ctx, 1)
	if err != nil || len(pop.Mangas) != 1 || pop.Mangas[0].Title != "One Piece" {
		t.Fatalf("popular: %v %+v", err, pop)
	}
	latest, err := m.Latest(ctx, 1)
	if err != nil || len(latest.Mangas) != 3 || latest.Mangas[0].Title != "Newest" || latest.Mangas[2].Title != "Older" {
		t.Fatalf("latest, newest update first: %v %+v", err, latest)
	}

	d, err := m.Details(ctx, sourcekit.Ref{URL: "#/titles/100020"})
	if err != nil || d.Title != "One Piece" || d.Author != "Eiichiro Oda" || d.Status != sourcekit.StatusOngoing ||
		d.Description != "Gol D. Roger was known as the Pirate King.\n\nThis series is updated every Sunday." ||
		len(d.Genres) != 2 || d.WebURL != "https://mangaplus.shueisha.co.jp/titles/100020" {
		t.Fatalf("details: %v %+v", err, d)
	}
	if d, err := m.Details(ctx, sourcekit.Ref{URL: "https://mangaplus.shueisha.co.jp/titles/100300"}); err != nil ||
		d.Status != sourcekit.StatusCompleted {
		t.Fatalf("a finished one-shot: %v %+v", err, d)
	}
	if _, err := m.Details(ctx, sourcekit.Ref{URL: "#/titles/100021"}); !errors.Is(err, sourcekit.ErrNotFound) {
		t.Fatalf("a Spanish title in the English catalog: %v", err)
	}
	if _, err := m.Details(ctx, sourcekit.Ref{URL: "#/titles/999"}); !errors.Is(err, sourcekit.ErrNotFound) {
		t.Fatalf("a removed title: %v", err)
	}

	chs, err := m.Chapters(ctx, sourcekit.Ref{URL: "#/titles/100020"})
	if err != nil || len(chs) != 3 {
		t.Fatalf("chapters (the expired one left out): %v %+v", err, chs)
	}
	if c := chs[0]; c.Name != "Ex - Special" || c.Number != -1 || c.URL != "#/viewer/1000487" {
		t.Fatalf("newest chapter %+v", c)
	}
	if c := chs[1]; c.Name != "#1100 - Thank You, Bonney" || c.Number != 1100 || c.Scanlator != "MANGA Plus" ||
		c.UploadedAt == nil || c.UploadedAt.Unix() != 1783633137 || c.WebURL != "https://mangaplus.shueisha.co.jp/viewer/1000486" {
		t.Fatalf("chapter %+v", c)
	}
	if chs[2].Number != 1 {
		t.Fatalf("oldest chapter %+v", chs[2])
	}
	_ = m.SetOption("subtitle_only", true)
	if chs, _ := m.Chapters(ctx, sourcekit.Ref{URL: "#/titles/100020"}); len(chs) != 3 || chs[1].Name != "Thank You, Bonney" {
		t.Fatalf("subtitle only: %+v", chs)
	}

	pages, err := m.Pages(ctx, sourcekit.PageRef{URL: chs[1].URL})
	if err != nil || len(pages) != 2 {
		t.Fatalf("pages: %v %+v", err, pages)
	}
	if p := pages[0]; p.URL != "https://cdn.example/p1.jpg?key=x" || p.Decode != "0a0b0c" || p.Headers["Plus-Vw-Token"] != "view-token" {
		t.Fatalf("page %+v", p)
	}
}

// TestMangaPlusDecode: a page is XORed with its key, repeated; doing it again
// gives the image back.
func TestMangaPlusDecode(t *testing.T) {
	m := &mangaplus{}
	plain := []byte("\xff\xd8\xff\xe0 a JPEG, or near enough, longer than the key")
	enc, err := mpXOR("0a0b0c", plain)
	if err != nil || bytes.Equal(enc, plain) || enc[0] != 0xff^0x0a || enc[3] != 0xe0^0x0a {
		t.Fatalf("encrypt: %v % x", err, enc[:4])
	}
	got, err := m.DecodePage(context.Background(), "0a0b0c", enc)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("decode: %v %q", err, got)
	}
	for _, bad := range []string{"", "zz"} {
		if _, err := m.DecodePage(context.Background(), bad, enc); err == nil {
			t.Fatalf("key %q should be refused", bad)
		}
	}
}

// TestMangaPlusProto reads the wire format the API uses, including what a
// reader has to skip.
func TestMangaPlusProto(t *testing.T) {
	msg := mpEnc{}.num(1, 300).str(2, "name")
	msg = append(msg, 3<<3|5, 1, 2, 3, 4)             // a fixed32 to skip
	msg = append(msg, 4<<3|1, 1, 2, 3, 4, 5, 6, 7, 8) // a fixed64 to skip
	msg = msg.num(5, -1)                              // a negative int32, sign-extended
	m, err := mpParse(msg)
	if err != nil || m.num(1) != 300 || m.str(2) != "name" || m.num(5) != -1 {
		t.Fatalf("parse: %v %v", err, m)
	}
	if _, ok := m.has(9); ok {
		t.Fatal("an absent field is absent")
	}
	if _, err := mpParse([]byte{2<<3 | 2, 10, 'x'}); err == nil {
		t.Fatal("a truncated field should be refused")
	}
}

// TestMangaPlusLanguages: a catalog per language, each with the id Mihon
// gives it and the API's own language name.
func TestMangaPlusLanguages(t *testing.T) {
	n := 0
	for _, s := range sourcekit.Build(sourcekit.Deps{Client: sourcekit.NewClient(nil)}) {
		if i := s.Info(); i.Name == "MANGA Plus" {
			n++
			if i.ID != sourcekit.KeiyoushiID("MANGA Plus by SHUEISHA", i.Lang, 1) {
				t.Errorf("%s catalog id %s", i.Lang, i.ID)
			}
		}
	}
	if n != len(mpLangs) {
		t.Fatalf("%d catalogs for %d languages", n, len(mpLangs))
	}
	if name, code := mpLang("pt-BR"); name != "ptb" || code != 4 {
		t.Fatalf("pt-BR is %s/%d", name, code)
	}
	if name, code := mpLang("vi"); name != "vie" || code != 9 {
		t.Fatalf("vi is %s/%d", name, code)
	}
}
