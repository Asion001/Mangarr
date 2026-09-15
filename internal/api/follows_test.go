package api_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/auth"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/testutil/fakelibrary"
)

// TestFollowsAndOwnTargets: users follow series they can see and keep
// their own notification targets, which can't reach the local network and
// stay out of the install's module list.
func TestFollowsAndOwnTargets(t *testing.T) {
	srv, a := newServer(t, false)
	ctx := context.Background()
	now := time.Now().UTC()
	rf := &model.RootFolder{Path: t.TempDir(), CreatedAt: now}
	if _, err := a.DB.NewInsert().Model(rf).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	tag := &model.Tag{Label: "adults"}
	if _, err := a.DB.NewInsert().Model(tag).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	open := &model.Series{Title: "Open", SortTitle: "open", Status: model.StatusOngoing, RootFolderID: rf.ID, Path: "Open", ProfileID: 1, Tags: []int64{}, AddedAt: now, UpdatedAt: now}
	hidden := &model.Series{Title: "Hidden", SortTitle: "hidden", Status: model.StatusOngoing, RootFolderID: rf.ID, Path: "Hidden", ProfileID: 1, Tags: []int64{tag.ID}, AddedAt: now, UpdatedAt: now}
	for _, s := range []*model.Series{open, hidden} {
		if _, err := a.DB.NewInsert().Model(s).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.Auth.CreateUser(ctx, auth.NewUser{Username: "boss", Password: "boss-pass-1"}); err != nil {
		t.Fatal(err)
	}
	kids := &model.Group{Name: "Kids", Permissions: []string{"requests.create"}, IncludeTags: []int64{}, ExcludeTags: []int64{tag.ID}, RootFolders: []int64{}, CreatedAt: now}
	if _, err := a.DB.NewInsert().Model(kids).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Auth.CreateUser(ctx, auth.NewUser{Username: "ann", Password: "ann-pass-1", GroupID: kids.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Auth.CreateUser(ctx, auth.NewUser{Username: "bob", Password: "bob-pass-1"}); err != nil {
		t.Fatal(err)
	}
	admin := caller{t, login(t, srv.URL, "boss", "boss-pass-1"), srv.URL}
	ann := caller{t, login(t, srv.URL, "ann", "ann-pass-1"), srv.URL}
	bob := caller{t, login(t, srv.URL, "bob", "bob-pass-1"), srv.URL}
	sid := func(s *model.Series) string { return strconv.FormatInt(s.ID, 10) }

	// follows
	if code := ann.do("PUT", "/api/v1/series/"+sid(open)+"/follow", "", nil); code != 204 {
		t.Fatalf("follow: %d", code)
	}
	if code := ann.do("PUT", "/api/v1/series/"+sid(hidden)+"/follow", "", nil); code != 404 {
		t.Fatalf("following a series you can't see: %d", code)
	}
	var got map[string]any
	ann.do("GET", "/api/v1/series/"+sid(open), "", &got)
	if got["following"] != true {
		t.Fatalf("series-get following = %v", got["following"])
	}
	var list []map[string]any
	bob.do("GET", "/api/v1/series", "", &list)
	for _, s := range list {
		if s["following"] == true {
			t.Fatal("bob sees ann's follow")
		}
	}
	if followers := a.Followers(ctx, open.ID); len(followers) != 1 {
		t.Fatalf("followers %v", followers)
	}
	ann.do("DELETE", "/api/v1/series/"+sid(open)+"/follow", "", nil)
	if followers := a.Followers(ctx, open.ID); len(followers) != 0 {
		t.Fatalf("followers after unfollowing %v", followers)
	}

	// own notification targets
	var created map[string]any
	body := `{"implementation":"webhook","name":"My hook","enabled":true,"events":["health.issue","request.updated"],"settings":{"url":"http://127.0.0.1:9/hook"}}`
	if code := ann.do("POST", "/api/v1/me/notifications", body, &created); code != 200 {
		t.Fatalf("create own target: %d", code)
	}
	id := strconv.FormatInt(int64(created["id"].(float64)), 10)
	if evs, _ := created["events"].([]any); len(evs) != 1 || evs[0] != "request.updated" {
		t.Fatalf("events not limited to personal ones: %v", created["events"])
	}
	var mine []map[string]any
	ann.do("GET", "/api/v1/me/notifications", "", &mine)
	if len(mine) != 1 {
		t.Fatalf("own targets %v", mine)
	}
	var mods []map[string]any
	admin.do("GET", "/api/v1/modules?kind=notify", "", &mods)
	if len(mods) != 0 {
		t.Fatalf("a user's target in the install's list: %v", mods)
	}
	if code := bob.do("PUT", "/api/v1/me/notifications/"+id, body, nil); code != 404 {
		t.Fatalf("bob editing ann's target: %d", code)
	}
	if code := bob.do("DELETE", "/api/v1/me/notifications/"+id, "", nil); code != 404 {
		t.Fatalf("bob deleting ann's target: %d", code)
	}
	// the local network is off limits
	req := `{"id":` + id + `,"implementation":"webhook","name":"My hook","enabled":true,"settings":{"url":"http://127.0.0.1:9/hook"}}`
	resp, err := ann.c.Post(srv.URL+"/api/v1/me/notifications/test", "application/json", strings.NewReader(req))
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 2048)
	n, _ := resp.Body.Read(b)
	resp.Body.Close()
	if resp.StatusCode != 400 || !strings.Contains(string(b[:n]), "address not allowed") {
		t.Fatalf("test to a local address: %d %s", resp.StatusCode, b[:n])
	}
	// editing keeps it ann's
	if code := ann.do("PUT", "/api/v1/me/notifications/"+id, strings.Replace(body, "My hook", "Renamed", 1), nil); code != 200 {
		t.Fatalf("update: %d", code)
	}
	ann.do("GET", "/api/v1/me/notifications", "", &mine)
	if len(mine) != 1 || mine[0]["name"] != "Renamed" {
		t.Fatalf("after update %v", mine)
	}
	if code := ann.do("DELETE", "/api/v1/me/notifications/"+id, "", nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
}

// TestOwnLibraryAccount: users link their own account on the library
// server to their own reader, and never see anyone else's.
func TestOwnLibraryAccount(t *testing.T) {
	srv, a := newServer(t, false)
	ctx := context.Background()
	fakelibrary.NewScenario("own-accounts")
	lib := &model.ProviderDefinition{Kind: "library", Implementation: "fakelibrary", Name: "Komga", Enabled: true, Settings: map[string]any{"scenario": "own-accounts"}}
	if err := a.Modules.Create(ctx, lib); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Auth.CreateUser(ctx, auth.NewUser{Username: "boss", Password: "boss-pass-1"}); err != nil {
		t.Fatal(err)
	}
	annUser, _ := a.Auth.CreateUser(ctx, auth.NewUser{Username: "ann", Password: "ann-pass-1"})
	_, _ = a.Auth.CreateUser(ctx, auth.NewUser{Username: "bob", Password: "bob-pass-1"})
	ann := caller{t, login(t, srv.URL, "ann", "ann-pass-1"), srv.URL}
	bob := caller{t, login(t, srv.URL, "bob", "bob-pass-1"), srv.URL}

	var list []map[string]any
	ann.do("GET", "/api/v1/me/library-accounts", "", &list)
	if len(list) != 1 || list[0]["name"] != "Komga" || list[0]["linked"] != nil {
		t.Fatalf("servers %v", list)
	}
	mid := strconv.FormatInt(lib.ID, 10)
	if code := ann.do("POST", "/api/v1/me/library-accounts", `{"moduleId":`+mid+`,"credentials":{"apiKey":""}}`, nil); code != 400 {
		t.Fatalf("bad credentials: %d", code)
	}
	if code := ann.do("POST", "/api/v1/me/library-accounts", `{"moduleId":`+mid+`,"credentials":{"apiKey":"k1"}}`, nil); code != 204 {
		t.Fatalf("link: %d", code)
	}
	ann.do("GET", "/api/v1/me/library-accounts", "", &list)
	if linked, _ := list[0]["linked"].(map[string]any); linked == nil || linked["externalUser"] != "user-k1" {
		t.Fatalf("after linking %v", list)
	}
	var accs []model.ReaderAccount
	_ = a.DB.NewSelect().Model(&accs).Scan(ctx)
	if len(accs) != 1 || accs[0].ReaderID != annUser.ReaderID {
		t.Fatalf("accounts %+v (ann's reader %d)", accs, annUser.ReaderID)
	}
	list = nil // decoding into the old maps would keep ann's keys
	bob.do("GET", "/api/v1/me/library-accounts", "", &list)
	if list[0]["linked"] != nil {
		t.Fatalf("bob sees ann's account: %v", list)
	}
	if code := ann.do("DELETE", "/api/v1/me/library-accounts/"+mid, "", nil); code != 204 {
		t.Fatalf("unlink: %d", code)
	}
	if n, _ := a.DB.NewSelect().Model((*model.ReaderAccount)(nil)).Count(ctx); n != 0 {
		t.Fatalf("accounts left %d", n)
	}
}
