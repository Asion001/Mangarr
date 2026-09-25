package mediaserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Asion001/mangarr/internal/modules/httpx"
)

type Settings struct {
	URL    string `json:"url" label:"Server URL" type:"url" required:"true" order:"1"`
	APIKey string `json:"apiKey" label:"API key" secret:"true" required:"true" order:"2"`
}

func (s *Settings) Validate() error {
	u, err := url.Parse(s.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return errors.New("server URL must be HTTP(S), without credentials, query or fragment")
	}
	if strings.TrimSpace(s.APIKey) == "" || strings.ContainsAny(s.APIKey, "\r\n") {
		return errors.New("API key is required and must not contain newlines")
	}
	return nil
}

// Client preserves the manager's transport policy (admin targets may be on
// the LAN) but never forwards credentials through redirects.
func Client(in *http.Client) *http.Client {
	if in == nil {
		in = http.DefaultClient
	}
	c := *in
	c.Timeout = 5 * time.Second
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// Get deliberately omits upstream bodies and transport errors: they may echo
// credentials and Test errors are returned by the admin API and health checks.
func Get(ctx context.Context, c *http.Client, address string, headers map[string]string, out any) error {
	err := httpx.Do(ctx, c, http.MethodGet, address, headers, nil, out)
	if err == nil {
		return nil
	}
	var status *httpx.StatusError
	if errors.As(err, &status) {
		return fmt.Errorf("media server returned HTTP %d", status.Code)
	}
	return errors.New("media server request failed (check URL, connection and response)")
}
