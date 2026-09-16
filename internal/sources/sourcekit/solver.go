package sourcekit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Solver answers browser challenges (Cloudflare and friends) through a
// FlareSolverr instance, and keeps the cookies it hands back so the next
// requests go straight to the site.
type Solver struct {
	// URL of FlareSolverr, e.g. http://flaresolverr:8191.
	URL string
	// HTTP talks to FlareSolverr itself (not through the site's client).
	HTTP *http.Client
	// Timeout for one challenge.
	Timeout time.Duration

	mu     sync.Mutex
	solved map[string]time.Time // host -> when it was last solved
}

// NewSolver returns a solver, or nil when no address is configured.
func NewSolver(address string) *Solver {
	if address == "" {
		return nil
	}
	return &Solver{URL: address, HTTP: &http.Client{Timeout: 3 * time.Minute}, Timeout: 60 * time.Second,
		solved: map[string]time.Time{}}
}

// coolOff keeps one host's challenges from being solved over and over when a
// site refuses for another reason.
const coolOff = 2 * time.Minute

type flareRequest struct {
	Cmd        string `json:"cmd"`
	URL        string `json:"url"`
	MaxTimeout int    `json:"maxTimeout"`
}

type flareResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Solution struct {
		URL       string `json:"url"`
		Status    int    `json:"status"`
		UserAgent string `json:"userAgent"`
		Cookies   []struct {
			Name   string  `json:"name"`
			Value  string  `json:"value"`
			Domain string  `json:"domain"`
			Path   string  `json:"path"`
			Expiry float64 `json:"expires"`
		} `json:"cookies"`
	} `json:"solution"`
}

// Solve asks FlareSolverr for target and stores the cookies (and the user
// agent that goes with them) in the client.
func (s *Solver) Solve(ctx context.Context, c *Client, target string) error {
	if s == nil || s.URL == "" {
		return fmt.Errorf("no challenge solver is configured")
	}
	u, err := url.Parse(target)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if at, ok := s.solved[u.Host]; ok && time.Since(at) < coolOff {
		s.mu.Unlock()
		return fmt.Errorf("%s was solved a moment ago and still refuses", u.Host)
	}
	s.mu.Unlock()

	body, _ := json.Marshal(flareRequest{Cmd: "request.get", URL: target, MaxTimeout: int(s.Timeout / time.Millisecond)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL+"/v1", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out flareResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	if out.Status != "ok" {
		return fmt.Errorf("%s", out.Message)
	}

	jar := c.HTTP.Jar
	if jar == nil {
		return fmt.Errorf("the site's client keeps no cookies")
	}
	var cookies []*http.Cookie
	for _, k := range out.Solution.Cookies {
		ck := &http.Cookie{Name: k.Name, Value: k.Value, Path: k.Path, Domain: k.Domain}
		if k.Expiry > 0 {
			ck.Expires = time.Unix(int64(k.Expiry), 0)
		}
		cookies = append(cookies, ck)
	}
	jar.SetCookies(u, cookies)
	if out.Solution.UserAgent != "" {
		c.UserAgent = out.Solution.UserAgent // the cookies are tied to it
	}
	s.mu.Lock()
	s.solved[u.Host] = time.Now()
	s.mu.Unlock()
	return nil
}
