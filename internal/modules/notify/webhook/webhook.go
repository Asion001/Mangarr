// Package webhook posts notification events as JSON to any URL.
package webhook

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
	URL     string            `json:"url" label:"URL" type:"url" required:"true" order:"1"`
	Method  string            `json:"method" label:"Method" options:"POST:POST,PUT:PUT" order:"2"`
	Headers map[string]string `json:"headers" label:"Headers" type:"keyvalue" order:"3" advanced:"true"`
}

func init() {
	modules.Register(&modules.Implementation{
		Kind: modules.KindNotify, Name: "webhook", DisplayName: "Webhook",
		Description: "Posts a JSON document describing each event to any HTTP endpoint.",
		Settings:    func() any { return &Settings{Method: "POST", Headers: map[string]string{}} },
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
	method := strings.ToUpper(m.s.Method)
	if method != http.MethodPut {
		method = http.MethodPost
	}
	payload := map[string]any{"event": msg.Event, "title": msg.Title, "body": msg.Body, "items": msg.Items,
		"url": msg.URL, "imageUrl": msg.ImageURL, "seriesId": msg.SeriesID, "series": msg.Series, "time": time.Now().UTC()}
	return httpx.Do(ctx, m.http, method, m.s.URL, m.s.Headers, payload, nil)
}
