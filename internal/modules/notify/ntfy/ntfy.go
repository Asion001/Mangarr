// Package ntfy publishes notifications to an ntfy topic.
package ntfy

import (
	"context"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/httpx"
	"github.com/Asion001/mangarr/internal/modules/notify"
)

type Settings struct {
	ServerURL string   `json:"serverUrl" label:"Server URL" type:"url" required:"true" order:"1"`
	Topic     string   `json:"topic" label:"Topic" required:"true" order:"2"`
	Token     string   `json:"token" label:"Access token" secret:"true" order:"3" advanced:"true"`
	Username  string   `json:"username" label:"Username" order:"4" advanced:"true"`
	Password  string   `json:"password" label:"Password" secret:"true" order:"5" advanced:"true"`
	Priority  int      `json:"priority" label:"Priority" options:"1:Min,2:Low,3:Default,4:High,5:Max" order:"6"`
	Tags      []string `json:"tags" label:"Tags" type:"tags" order:"7" help:"ntfy tags/emojis, e.g. books"`
}

func init() {
	modules.Register(&modules.Implementation{
		Kind: modules.KindNotify, Name: "ntfy", DisplayName: "ntfy",
		Description: "Push notifications through ntfy.sh or a self-hosted ntfy server.",
		Settings:    func() any { return &Settings{ServerURL: "https://ntfy.sh", Priority: 3, Tags: []string{"books"}} },
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
	h := map[string]string{"Title": msg.Title, "Priority": strconv.Itoa(max(1, min(5, m.s.Priority)))}
	if len(m.s.Tags) > 0 {
		h["Tags"] = strings.Join(m.s.Tags, ",")
	}
	if msg.URL != "" {
		h["Click"] = msg.URL
	}
	if msg.ImageURL != "" {
		h["Icon"] = msg.ImageURL
	}
	switch {
	case m.s.Token != "":
		h["Authorization"] = "Bearer " + m.s.Token
	case m.s.Username != "":
		h["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(m.s.Username+":"+m.s.Password))
	}
	body := msg.Text()
	if body == "" {
		body = msg.Title
	}
	return httpx.Do(ctx, m.http, http.MethodPost, httpx.Join(m.s.ServerURL, m.s.Topic), h, body, nil)
}
