// Package gotify sends notifications to a Gotify server.
package gotify

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/httpx"
	"github.com/Asion001/mangarr/internal/modules/notify"
)

type Settings struct {
	ServerURL string `json:"serverUrl" label:"Server URL" type:"url" required:"true" order:"1"`
	AppToken  string `json:"appToken" label:"Application token" secret:"true" required:"true" order:"2"`
	Priority  int    `json:"priority" label:"Priority" order:"3" help:"0-10"`
}

func init() {
	modules.Register(&modules.Implementation{
		Kind: modules.KindNotify, Name: "gotify", DisplayName: "Gotify",
		Description: "Notifications to a self-hosted Gotify server.",
		Settings:    func() any { return &Settings{Priority: 5} },
		New: func(deps modules.Deps, s any) (modules.Instance, error) {
			hc := deps.HTTP
			if hc == nil {
				hc = &http.Client{Timeout: 30 * time.Second}
			}
			return &Module{s: s.(*Settings), http: hc}, nil
		},
	})
}

type Module struct {
	s    *Settings
	http *http.Client
}

func (m *Module) Test(ctx context.Context) error {
	return m.Send(ctx, notify.Message{Event: "test", Title: "mangarr", Body: "Test notification"})
}

func (m *Module) Send(ctx context.Context, msg notify.Message) error {
	payload := map[string]any{"title": msg.Title, "message": msg.Text(), "priority": m.s.Priority,
		"extras": map[string]any{"client::display": map[string]any{"contentType": "text/plain"}}}
	if msg.URL != "" {
		payload["extras"].(map[string]any)["client::notification"] = map[string]any{"click": map[string]any{"url": msg.URL}}
	}
	u := httpx.Join(m.s.ServerURL, "/message") + "?token=" + url.QueryEscape(m.s.AppToken)
	return httpx.Do(ctx, m.http, http.MethodPost, u, nil, payload, nil)
}
