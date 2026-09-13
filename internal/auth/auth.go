// Package auth handles users, cookie sessions and API-key authentication.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/settings"
)

const CookieName = "mangarr_session"
const sessionTTL = 30 * 24 * time.Hour

var ErrInvalidCredentials = errors.New("invalid username or password")

type Service struct {
	db       *db.DB
	settings *settings.Store
	disabled bool
}

func NewService(d *db.DB, s *settings.Store, disabled bool) *Service {
	return &Service{db: d, settings: s, disabled: disabled}
}

func (s *Service) Disabled() bool { return s.disabled }

func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	n, err := s.db.NewSelect().Model((*model.User)(nil)).Count(ctx)
	return n == 0, err
}

// CreateUser creates a user; used by first-run setup.
func (s *Service) CreateUser(ctx context.Context, username, password string) (*model.User, error) {
	username = strings.TrimSpace(username)
	if username == "" || len(password) < 6 {
		return nil, fmt.Errorf("username required and password must be at least 6 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	u := &model.User{Username: username, PasswordHash: string(hash), CreatedAt: time.Now().UTC()}
	if _, err := s.db.NewInsert().Model(u).Exec(ctx); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Service) ChangePassword(ctx context.Context, username, password string) error {
	if len(password) < 6 {
		return fmt.Errorf("password must be at least 6 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.NewUpdate().Model((*model.User)(nil)).Set("password_hash = ?", string(hash)).Where("username = ?", username).Exec(ctx)
	return err
}

func (s *Service) Verify(ctx context.Context, username, password string) (*model.User, error) {
	var u model.User
	err := s.db.NewSelect().Model(&u).Where("username = ?", username).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$invalidinvalidinvalidinvalidinvalidinvalidinvalidinva"), []byte(password))
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	return &u, nil
}

// SessionCookie creates a signed session cookie for username.
func (s *Service) SessionCookie(ctx context.Context, username string, secure bool) (*http.Cookie, error) {
	g, err := s.settings.General(ctx)
	if err != nil {
		return nil, err
	}
	exp := time.Now().Add(sessionTTL).Unix()
	payload := username + "|" + strconv.FormatInt(exp, 10)
	sig := sign(g.SessionSecret, payload)
	val := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig
	return &http.Cookie{Name: CookieName, Value: val, Path: "/", HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteLaxMode, Expires: time.Unix(exp, 0)}, nil
}

func sign(secret, payload string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Authenticate returns the principal for a request, or "" when unauthenticated.
func (s *Service) Authenticate(r *http.Request) string {
	if s.disabled {
		return "anonymous"
	}
	ctx := r.Context()
	g, err := s.settings.General(ctx)
	if err != nil {
		return ""
	}
	key := r.Header.Get("X-Api-Key")
	if key == "" {
		key = r.URL.Query().Get("apikey")
	}
	if key == "" {
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			key = strings.TrimPrefix(h, "Bearer ")
		}
	}
	if key != "" && g.APIKey != "" && subtle.ConstantTimeCompare([]byte(key), []byte(g.APIKey)) == 1 {
		return "apikey"
	}
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	enc, sig, ok := strings.Cut(c.Value, ".")
	if !ok {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return ""
	}
	payload := string(raw)
	if !hmac.Equal([]byte(sign(g.SessionSecret, payload)), []byte(sig)) {
		return ""
	}
	user, expStr, ok := strings.Cut(payload, "|")
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if !ok || err != nil || time.Now().Unix() > exp {
		return ""
	}
	return user
}

type ctxKey struct{}

func WithPrincipal(ctx context.Context, p string) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

func Principal(ctx context.Context) string {
	p, _ := ctx.Value(ctxKey{}).(string)
	return p
}
