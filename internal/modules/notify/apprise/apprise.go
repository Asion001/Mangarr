// Package apprise sends notifications through an Apprise API server, which
// fans out to 100+ services.
package apprise

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/httpx"
	"github.com/Asion001/mangarr/internal/modules/notify"
)

type Settings struct {
	ServerURL string   `json:"serverUrl" label:"Apprise API URL" type:"url" required:"true" placeholder:"http://apprise:8000" order:"1"`
	ConfigKey string   `json:"configKey" label:"Configuration key" order:"2" help:"Use a stored Apprise configuration (stateful)."`
	URLs      []string `json:"urls" label:"Notification URLs" type:"tags" secret:"false" order:"3" help:"Apprise URLs for stateless mode (used when no configuration key is set)."`
	Tags      []string `json:"tags" label:"Apprise tags" type:"tags" order:"4" advanced:"true"`
}

func (s *Settings) Validate() error {
	if s.ConfigKey == "" && len(s.URLs) == 0 {
		return errors.New("set a configuration key or at least one notification URL")
	}
	return nil
}

func init() {
	modules.Register(&modules.Implementation{
		Kind: modules.KindNotify, Name: "apprise", DisplayName: "Apprise",
		Description: "Any service supported by Apprise, through an Apprise API server.",
		Settings:    func() any { return &Settings{} },
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
	typ := "info"
	switch msg.Event {
	case "download.failed", "health.issue":
		typ = "warning"
	case "chapter.imported", "health.restored":
		typ = "success"
	}
	payload := map[string]any{"title": msg.Title, "body": msg.Text(), "type": typ}
	if len(m.s.Tags) > 0 {
		payload["tag"] = strings.Join(m.s.Tags, ",")
	}
	u := httpx.Join(m.s.ServerURL, "/notify")
	if m.s.ConfigKey != "" {
		u = httpx.Join(u, m.s.ConfigKey)
	} else {
		payload["urls"] = strings.Join(m.s.URLs, ",")
	}
	return httpx.Do(ctx, m.http, http.MethodPost, u, nil, payload, nil)
}
