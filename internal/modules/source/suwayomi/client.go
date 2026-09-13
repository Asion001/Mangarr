package suwayomi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// client is a minimal GraphQL + REST client for Suwayomi-Server.
type client struct {
	base     *url.URL
	http     *http.Client
	username string
	password string
}

type gqlError struct {
	Message string `json:"message"`
}

// GraphQLError holds the (trimmed) error messages returned by the server.
type GraphQLError struct{ Messages []string }

func (e *GraphQLError) Error() string { return "suwayomi: " + strings.Join(e.Messages, "; ") }

// Contains reports whether any message contains s.
func (e *GraphQLError) Contains(s string) bool {
	for _, m := range e.Messages {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}

// do runs a GraphQL operation. Data is decoded into out even when errors are
// present (partial results); errors are returned as *GraphQLError.
func (c *client) do(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("/api/graphql"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("suwayomi: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("suwayomi: unauthorized (check username/password)")
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []gqlError      `json:"errors"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("suwayomi: HTTP %d: %s", resp.StatusCode, snippet(raw))
	}
	if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("suwayomi: decode: %w", err)
		}
	}
	if len(env.Errors) > 0 {
		ge := &GraphQLError{}
		for _, e := range env.Errors {
			ge.Messages = append(ge.Messages, trimTrace(e.Message))
		}
		return ge
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("suwayomi: HTTP %d: %s", resp.StatusCode, snippet(raw))
	}
	return nil
}

// get performs an authenticated GET on a server path (e.g. page images).
func (c *client) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(path), nil)
	if err != nil {
		return nil, err
	}
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("suwayomi: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, fmt.Errorf("suwayomi: GET %s: HTTP %d: %s", path, resp.StatusCode, snippet(b))
	}
	return resp, nil
}

func (c *client) url(path string) string {
	u := *c.base
	p, q, _ := strings.Cut(path, "?")
	u.Path = strings.TrimRight(u.Path, "/") + p
	u.RawQuery = q
	return u.String()
}

func (c *client) auth(r *http.Request) {
	if c.username != "" {
		r.SetBasicAuth(c.username, c.password)
	}
}

// trimTrace keeps the first line of a JVM exception message.
func trimTrace(s string) string {
	s = strings.TrimPrefix(s, "Exception while fetching data ")
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
