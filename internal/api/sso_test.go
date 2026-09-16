package api_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/auth"
	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/settings"
)

// fakeIDP is a small OpenID Connect provider for the tests: it signs in
// whoever it is told to, with PKCE and a nonce like a real one.
type fakeIDP struct {
	*httptest.Server
	key *rsa.PrivateKey

	mu sync.Mutex
	// Subject, Username and Groups are what the next sign-in returns.
	Subject  string
	Username string
	Name     string
	Groups   []string
	codes    map[string]authReq
}

type authReq struct {
	nonce     string
	challenge string
	redirect  string
	subject   string
	username  string
	name      string
	groups    []string
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIDP{key: key, codes: map[string]authReq{}}
	mux := http.NewServeMux()
	idp.Server = httptest.NewServer(mux)
	t.Cleanup(idp.Close)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": idp.URL, "authorization_endpoint": idp.URL + "/authorize", "token_endpoint": idp.URL + "/token",
			"jwks_uri": idp.URL + "/jwks", "userinfo_endpoint": idp.URL + "/userinfo",
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{
			"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "test",
			"n": b64(key.PublicKey.N.Bytes()), "e": b64(big.NewInt(int64(key.PublicKey.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		idp.mu.Lock()
		code := "code-" + q.Get("state")[:8]
		idp.codes[code] = authReq{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"),
			subject: idp.Subject, username: idp.Username, name: idp.Name, groups: idp.Groups}
		idp.mu.Unlock()
		to, _ := url.Parse(q.Get("redirect_uri"))
		rq := to.Query()
		rq.Set("code", code)
		rq.Set("state", q.Get("state"))
		to.RawQuery = rq.Encode()
		http.Redirect(w, r, to.String(), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		idp.mu.Lock()
		req, ok := idp.codes[r.Form.Get("code")]
		delete(idp.codes, r.Form.Get("code"))
		idp.mu.Unlock()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || b64(sum[:]) != req.challenge {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		clientID := r.Form.Get("client_id")
		if id, _, ok := r.BasicAuth(); ok { // oauth2 sends the credentials as basic auth
			clientID = id
		}
		claims := map[string]any{"iss": idp.URL, "sub": req.subject, "aud": clientID, "nonce": req.nonce,
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
			"preferred_username": req.username, "name": req.name, "groups": req.groups}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": idp.sign(claims)})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		idp.mu.Lock()
		defer idp.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"sub": idp.Subject, "preferred_username": idp.Username, "groups": idp.Groups})
	})
	return idp
}

// sign makes an RS256 JWT of the claims.
func (i *fakeIDP) sign(claims map[string]any) string {
	head, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "test"})
	body, _ := json.Marshal(claims)
	signing := b64(head) + "." + b64(body)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, i.key, crypto.SHA256, sum[:])
	if err != nil {
		panic(err)
	}
	return signing + "." + b64(sig)
}

// as sets who signs in next.
func (i *fakeIDP) as(subject, username, name string, groups ...string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.Subject, i.Username, i.Name, i.Groups = subject, username, name, groups
}

// signIn follows the whole flow and returns the page the browser lands on.
func signIn(t *testing.T, base, query string) (*http.Client, *url.URL) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	resp, err := c.Get(base + "/api/v1/auth/oidc/login" + query)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return c, resp.Request.URL
}

// TestSSO: signing in with a provider creates accounts, follows the
// provider's groups, works with an invite, and turns people away when it
// should.
func TestSSO(t *testing.T) {
	srv, a := newServer(t, false)
	ctx := context.Background()
	idp := newFakeIDP(t)
	if _, err := a.Auth.CreateUser(ctx, auth.NewUser{Username: "boss", Password: "boss-pass-1"}); err != nil {
		t.Fatal(err)
	}
	g, _ := a.Settings.General(ctx)
	g.PublicURL = srv.URL
	if err := a.Settings.Set(ctx, settings.KeyGeneral, g); err != nil {
		t.Fatal(err)
	}
	friends := &model.Group{Name: "Friends", Permissions: []string{"requests.create"}, IncludeTags: []int64{}, ExcludeTags: []int64{}, RootFolders: []int64{}, CreatedAt: time.Now().UTC()}
	kids := &model.Group{Name: "Kids", Permissions: []string{}, IncludeTags: []int64{}, ExcludeTags: []int64{}, RootFolders: []int64{}, CreatedAt: time.Now().UTC()}
	for _, x := range []*model.Group{friends, kids} {
		if _, err := a.DB.NewInsert().Model(x).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	cfg := settings.DefaultSSO()
	cfg.Enabled, cfg.Issuer, cfg.ClientID, cfg.ClientSecret, cfg.CreateUsers = true, idp.URL, "mangarr", "shh", true
	cfg.Groups = []settings.SSOGroup{{Claim: "manga-friends", GroupID: friends.ID}, {Claim: "manga-kids", GroupID: kids.ID}}
	save := func() {
		if err := a.Settings.Set(ctx, settings.KeySSO, cfg); err != nil {
			t.Fatal(err)
		}
	}
	save()

	// a first sign-in creates the account in the mapped group
	idp.as("sub-1", "ann@example.com", "Ann Example", "manga-friends", "other")
	client, landed := signIn(t, srv.URL, "?next=/series/3")
	if landed.Path != "/series/3" {
		t.Fatalf("landed on %s", landed)
	}
	var st map[string]any
	me := caller{t, client, srv.URL}
	me.do("GET", "/api/v1/auth/status", "", &st)
	acc, _ := st["account"].(map[string]any)
	if acc == nil || acc["username"] != "ann" || acc["group"] != "Friends" || acc["displayName"] != "Ann Example" {
		t.Fatalf("account after signing in: %v", st)
	}
	var u model.User
	if err := a.DB.NewSelect().Model(&u).Where("oidc_subject = ?", "sub-1").Scan(ctx); err != nil || u.PasswordHash != "" {
		t.Fatalf("user %+v %v", u, err)
	}

	// the group follows the provider's groups
	idp.as("sub-1", "ann@example.com", "Ann Example", "manga-kids")
	client, _ = signIn(t, srv.URL, "")
	me = caller{t, client, srv.URL}
	st = nil
	me.do("GET", "/api/v1/auth/status", "", &st)
	if acc, _ := st["account"].(map[string]any); acc == nil || acc["group"] != "Kids" {
		t.Fatalf("group after the provider changed it: %v", st)
	}
	if n, _ := a.DB.NewSelect().Model((*model.User)(nil)).Count(ctx); n != 2 {
		t.Fatalf("a second account was created: %d", n)
	}

	// people in no mapped group are turned away when only mapped ones may in
	cfg.OnlyMapped = true
	save()
	idp.as("sub-2", "mallory", "Mallory", "strangers")
	_, landed = signIn(t, srv.URL, "")
	if landed.Path != "/login" || landed.Query().Get("error") == "" {
		t.Fatalf("an unmapped account got in: %s", landed)
	}
	cfg.OnlyMapped = false
	save()

	// without creating accounts, only people with an invite get in
	cfg.CreateUsers = false
	save()
	idp.as("sub-3", "bob", "Bob", "strangers") // no mapping matches: the invite's group stands
	_, landed = signIn(t, srv.URL, "")
	if landed.Path != "/login" || !strings.Contains(landed.Query().Get("error"), "invite") {
		t.Fatalf("an account was created without an invite: %s", landed)
	}
	admin := caller{t, login(t, srv.URL, "boss", "boss-pass-1"), srv.URL}
	var inv map[string]any
	admin.do("POST", "/api/v1/invites", `{"groupId":`+strconv.FormatInt(kids.ID, 10)+`,"expireDays":7}`, &inv)
	token, _ := inv["token"].(string)
	client, landed = signIn(t, srv.URL, "?invite="+token)
	if landed.Path != "/" {
		t.Fatalf("invite sign-in landed on %s", landed)
	}
	st = nil
	caller{t, client, srv.URL}.do("GET", "/api/v1/auth/status", "", &st)
	if acc, _ := st["account"].(map[string]any); acc == nil || acc["username"] != "bob" || acc["group"] != "Kids" {
		t.Fatalf("account from the invite: %v", st)
	}

	// passwords off: only administrators may still use theirs
	cfg.PasswordLogin = false
	save()
	if code := (caller{t, http.DefaultClient, srv.URL}).do("POST", "/api/v1/auth/login", `{"username":"ann","password":"whatever"}`, nil); code != 401 {
		t.Fatalf("password login for an SSO account: %d", code)
	}
	if err := a.Auth.SetPassword(ctx, findUser(t, a.DB, "bob").ID, "bob-pass-1", ""); err != nil {
		t.Fatal(err)
	}
	if code := (caller{t, http.DefaultClient, srv.URL}).do("POST", "/api/v1/auth/login", `{"username":"bob","password":"bob-pass-1"}`, nil); code != 403 {
		t.Fatalf("password login while passwords are off: %d", code)
	}
	if code := (caller{t, http.DefaultClient, srv.URL}).do("POST", "/api/v1/auth/login", `{"username":"boss","password":"boss-pass-1"}`, nil); code != 200 {
		t.Fatalf("an administrator's password login: %d", code)
	}
	// and the settings never hand back the client secret
	var got map[string]any
	admin.do("GET", "/api/v1/settings/sso", "", &got)
	if got["clientSecret"] == "shh" || got["redirectUrl"] != srv.URL+"/api/v1/auth/oidc/callback" {
		t.Fatalf("sso settings %v", got)
	}
}

// findUser loads a user by name.
func findUser(t *testing.T, db *db.DB, name string) model.User {
	t.Helper()
	var u model.User
	if err := db.NewSelect().Model(&u).Where("username = ?", name).Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	return u
}
