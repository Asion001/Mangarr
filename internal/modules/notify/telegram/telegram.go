// Package telegram sends notifications through a Telegram bot.
package telegram

import (
	"context"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/httpx"
	"github.com/Asion001/mangarr/internal/modules/notify"
)

type Settings struct {
	BotToken  string `json:"botToken" label:"Bot token" secret:"true" required:"true" order:"1" help:"From @BotFather."`
	ChatID    string `json:"chatId" label:"Chat ID" required:"true" order:"2" help:"User, group or channel id (e.g. -1001234567890)."`
	TopicID   int    `json:"topicId" label:"Topic ID" order:"3" advanced:"true" help:"Forum topic (message thread) id, optional."`
	Silent    bool   `json:"silent" label:"Send silently" order:"4"`
	SendCover bool   `json:"sendCover" label:"Attach cover image" order:"5"`
	APIURL    string `json:"apiUrl" label:"API URL" order:"6" advanced:"true"`
}

func init() {
	modules.Register(&modules.Implementation{
		Kind: modules.KindNotify, Name: "telegram", DisplayName: "Telegram",
		Description: "Messages from a Telegram bot to a user, group, channel or forum topic.",
		Settings:    func() any { return &Settings{SendCover: true, APIURL: "https://api.telegram.org"} },
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
	return m.Send(ctx, notify.Message{Event: "test", Title: "mangarr", Body: "Test notification ✅"})
}

func (m *Module) Send(ctx context.Context, msg notify.Message) error {
	var b strings.Builder
	b.WriteString("<b>" + html.EscapeString(msg.Title) + "</b>")
	if msg.Body != "" {
		b.WriteString("\n" + html.EscapeString(msg.Body))
	}
	for _, it := range msg.Items {
		b.WriteString("\n• " + html.EscapeString(it))
	}
	if msg.URL != "" {
		b.WriteString("\n<a href=\"" + html.EscapeString(msg.URL) + "\">Open in mangarr</a>")
	}
	text := b.String()
	payload := map[string]any{"chat_id": m.s.ChatID, "parse_mode": "HTML", "disable_notification": m.s.Silent}
	if m.s.TopicID > 0 {
		payload["message_thread_id"] = m.s.TopicID
	}
	method := "sendMessage"
	if m.s.SendCover && msg.ImageURL != "" && len(text) <= 1024 {
		method = "sendPhoto"
		payload["photo"], payload["caption"] = msg.ImageURL, text
	} else {
		payload["text"] = text
		payload["link_preview_options"] = map[string]any{"is_disabled": true}
	}
	url := strings.TrimRight(m.s.APIURL, "/") + "/bot" + m.s.BotToken + "/" + method
	err := httpx.Do(ctx, m.http, http.MethodPost, url, nil, payload, nil)
	if err != nil && method == "sendPhoto" {
		// photo URLs can be rejected (size/format): retry as text
		delete(payload, "photo")
		delete(payload, "caption")
		payload["text"] = text
		return httpx.Do(ctx, m.http, http.MethodPost, strings.TrimRight(m.s.APIURL, "/")+"/bot"+m.s.BotToken+"/sendMessage", nil, payload, nil)
	}
	return err
}
