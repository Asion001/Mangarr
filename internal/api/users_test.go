package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/Asion001/mangarr/internal/auth"
)

type caller struct {
	t *testing.T
	c *http.Client
	b string
}

func (c caller) do(method, path, body string, out any) int {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.b+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.c.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode < 300 {
		b, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(b, out); err != nil {
			c.t.Fatalf("%s %s: %v %s", method, path, err, b)
		}
	}
	return resp.StatusCode
}

// TestInvitesAndGroups: an admin invites a friend into a group; the link
// works once; groups and users can be edited but never leave the install
// without an admin.
func TestInvitesAndGroups(t *testing.T) {
	srv, a := newServer(t, false)
	ctx := context.Background()
	if _, err := a.Auth.CreateUser(ctx, auth.NewUser{Username: "boss", Password: "boss-pass-1"}); err != nil {
		t.Fatal(err)
	}
	admin := caller{t, login(t, srv.URL, "boss", "boss-pass-1"), srv.URL}
	anon := caller{t, http.DefaultClient, srv.URL}

	var perms []map[string]string
	admin.do("GET", "/api/v1/permissions", "", &perms)
	if len(perms) != 6 {
		t.Fatalf("permissions %v", perms)
	}
	var g map[string]any
	if code := admin.do("POST", "/api/v1/groups", `{"name":"Kids","permissions":["apps"],"includeTags":[1]}`, &g); code != 200 {
		t.Fatalf("group: %d", code)
	}
	kids := int64(g["id"].(float64))
	if code := admin.do("POST", "/api/v1/groups", `{"name":"Bad","permissions":["root"]}`, nil); code != 400 {
		t.Fatalf("unknown permission: %d", code)
	}

	var inv map[string]any
	admin.do("POST", "/api/v1/invites", `{"groupId":`+strconv.FormatInt(kids, 10)+`,"note":"for Ann","expireDays":7}`, &inv)
	token, _ := inv["token"].(string)
	if token == "" || inv["group"] != "Kids" || inv["active"] != true {
		t.Fatalf("invite %v", inv)
	}
	var info map[string]any
	if code := anon.do("GET", "/api/v1/invites/redeem/"+token, "", &info); code != 200 || info["group"] != "Kids" || info["note"] != "for Ann" {
		t.Fatalf("invite info %d %v", code, info)
	}
	if code := anon.do("GET", "/api/v1/invites/redeem/nope", "", nil); code != 404 {
		t.Fatalf("bad token: %d", code)
	}
	// too short a password gives the use back
	if code := anon.do("POST", "/api/v1/invites/redeem/"+token, `{"username":"ann","password":"short"}`, nil); code != 422 && code != 400 {
		t.Fatalf("short password: %d", code)
	}
	var st map[string]any
	if code := anon.do("POST", "/api/v1/invites/redeem/"+token, `{"username":"ann","password":"ann-pass-1"}`, &st); code != 200 {
		t.Fatalf("redeem: %d", code)
	}
	acc := st["account"].(map[string]any)
	if acc["group"] != "Kids" || acc["readerId"].(float64) == 0 {
		t.Fatalf("account %v", acc)
	}
	if code := anon.do("POST", "/api/v1/invites/redeem/"+token, `{"username":"bob","password":"bob-pass-1"}`, nil); code != 404 {
		t.Fatalf("single-use invite used twice: %d", code)
	}
	friend := caller{t, login(t, srv.URL, "ann", "ann-pass-1"), srv.URL}
	if code := friend.do("GET", "/api/v1/users", "", nil); code != 403 {
		t.Fatalf("friend listing users: %d", code)
	}

	var users []map[string]any
	admin.do("GET", "/api/v1/users", "", &users)
	if len(users) != 2 {
		t.Fatalf("users %v", users)
	}
	var bossID, annID int64
	for _, u := range users {
		switch u["username"] {
		case "boss":
			bossID = int64(u["id"].(float64))
		case "ann":
			annID = int64(u["id"].(float64))
			if u["group"] != "Kids" || u["sessions"].(float64) != 2 {
				t.Fatalf("ann %v", u)
			}
		}
	}
	var groups []map[string]any
	admin.do("GET", "/api/v1/groups", "", &groups)
	usersGroup := int64(0)
	for _, x := range groups {
		if x["builtin"] == "users" {
			usersGroup = int64(x["id"].(float64))
		}
	}
	// the only admin can't lose the role or be deleted
	if code := admin.do("PUT", "/api/v1/users/"+strconv.FormatInt(bossID, 10), `{"groupId":`+strconv.FormatInt(usersGroup, 10)+`}`, nil); code != 400 {
		t.Fatalf("demoting the last admin: %d", code)
	}
	if code := admin.do("DELETE", "/api/v1/users/"+strconv.FormatInt(bossID, 10), "", nil); code != 400 {
		t.Fatalf("deleting yourself: %d", code)
	}
	// disabling Ann signs her out
	if code := admin.do("PUT", "/api/v1/users/"+strconv.FormatInt(annID, 10), `{"groupId":`+strconv.FormatInt(kids, 10)+`,"disabled":true}`, nil); code != 204 {
		t.Fatalf("disable: %d", code)
	}
	if code := friend.do("GET", "/api/v1/series", "", nil); code != 401 {
		t.Fatalf("disabled user still signed in: %d", code)
	}
	// deleting the group moves members to Users
	if code := admin.do("DELETE", "/api/v1/groups/"+strconv.FormatInt(kids, 10), "", nil); code != 204 {
		t.Fatalf("delete group: %d", code)
	}
	admin.do("GET", "/api/v1/users", "", &users)
	for _, u := range users {
		if u["username"] == "ann" && u["group"] != "Users" {
			t.Fatalf("ann after the group was deleted: %v", u)
		}
	}
}
