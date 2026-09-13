// Package discord sends notifications to a Discord webhook.
package discord

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/httpx"
	"github.com/Asion001/mangarr/internal/modules/notify"
)

type Settings struct {
	WebhookURL string `json:"webhookUrl" label:"Webhook URL" secret:"true" required:"true" order:"1"`
	Username   string `json:"username" label:"Username" order:"2"`
	AvatarURL  string `json:"avatarUrl" label:"Avatar URL" type:"url" order:"3" advanced:"true"`
}

func init() {
	modules.Register(&modules.Implementation{
		Kind: modules.KindNotify, Name: "discord", DisplayName: "Discord",
		Description: "Rich embeds posted to a Discord channel webhook.",
		Settings:    func() any { return &Settings{Username: "mangarr"} },
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

var colors = map[string]int{"chapter.imported": 0x2ecc71, "chapter.upgraded": 0x3498db, "download.failed": 0xe74c3c, "health.issue": 0xe67e22, "health.restored": 0x2ecc71}

func (m *Module) Test(ctx context.Context) error {
	return m.Send(ctx, notify.Message{Event: "test", Title: "mangarr", Body: "Test notification"})
}

func (m *Module) Send(ctx context.Context, msg notify.Message) error {
	desc := msg.Body
	if len(msg.Items) > 0 {
		desc += "\n• " + strings.Join(msg.Items, "\n• ")
	}
	if len(desc) > 4000 {
		desc = desc[:4000] + "…"
	}
	embed := map[string]any{"title": msg.Title, "description": strings.TrimSpace(desc), "color": colors[msg.Event]}
	if msg.URL != "" {
		embed["url"] = msg.URL
	}
	if msg.ImageURL != "" {
		embed["thumbnail"] = map[string]any{"url": msg.ImageURL}
	}
	payload := map[string]any{"embeds": []any{embed}}
	if m.s.Username != "" {
		payload["username"] = m.s.Username
	}
	if m.s.AvatarURL != "" {
		payload["avatar_url"] = m.s.AvatarURL
	}
	return httpx.Do(ctx, m.http, http.MethodPost, m.s.WebhookURL, nil, payload, nil)
}
