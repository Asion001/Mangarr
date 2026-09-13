package notify_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/notify"
	_ "github.com/Asion001/mangarr/internal/modules/notify/apprise"
	_ "github.com/Asion001/mangarr/internal/modules/notify/discord"
	_ "github.com/Asion001/mangarr/internal/modules/notify/gotify"
	_ "github.com/Asion001/mangarr/internal/modules/notify/ntfy"
	_ "github.com/Asion001/mangarr/internal/modules/notify/telegram"
	_ "github.com/Asion001/mangarr/internal/modules/notify/webhook"
)

type captured struct {
	method, path, body string
	header             http.Header
}

func TestNotifyModules(t *testing.T) {
	var got captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = captured{r.Method, r.URL.Path + "?" + r.URL.RawQuery, string(b), r.Header.Clone()}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	cases := []struct {
		impl     string
		settings map[string]any
		check    func(t *testing.T, c captured)
	}{
		{"telegram", map[string]any{"botToken": "TOKEN", "chatId": "42", "apiUrl": srv.URL, "sendCover": false}, func(t *testing.T, c captured) {
			if !strings.HasPrefix(c.path, "/botTOKEN/sendMessage") || !strings.Contains(c.body, `"chat_id":"42"`) || !strings.Contains(c.body, `\u0026lt;One\u0026gt;`) {
				t.Errorf("telegram: %+v", c)
			}
		}},
		{"discord", map[string]any{"webhookUrl": srv.URL + "/hook"}, func(t *testing.T, c captured) {
			if c.path != "/hook?" || !strings.Contains(c.body, `"embeds"`) || !strings.Contains(c.body, "Ch. 1") {
				t.Errorf("discord: %+v", c)
			}
		}},
		{"ntfy", map[string]any{"serverUrl": srv.URL, "topic": "manga", "token": "tk"}, func(t *testing.T, c captured) {
			if c.path != "/manga?" || c.header.Get("Title") != "<One> title" || c.header.Get("Authorization") != "Bearer tk" || !strings.Contains(c.body, "• Ch. 1") {
				t.Errorf("ntfy: %+v", c)
			}
		}},
		{"gotify", map[string]any{"serverUrl": srv.URL, "appToken": "abc"}, func(t *testing.T, c captured) {
			if c.path != "/message?token=abc" || !strings.Contains(c.body, "\"title\":\"\\u003cOne\\u003e title\"") || !strings.Contains(c.body, `"priority":5`) {
				t.Errorf("gotify: %+v", c)
			}
		}},
		{"apprise", map[string]any{"serverUrl": srv.URL, "configKey": "mykey"}, func(t *testing.T, c captured) {
			if c.path != "/notify/mykey?" || !strings.Contains(c.body, `"type":"success"`) {
				t.Errorf("apprise: %+v", c)
			}
		}},
		{"webhook", map[string]any{"url": srv.URL + "/wh", "headers": map[string]any{"X-Test": "1"}}, func(t *testing.T, c captured) {
			if c.path != "/wh?" || c.header.Get("X-Test") != "1" || !strings.Contains(c.body, `"event":"chapter.imported"`) {
				t.Errorf("webhook: %+v", c)
			}
		}},
	}
	msg := notify.Message{Event: "chapter.imported", Title: "<One> title", Body: "Chapter 1", Items: []string{"Ch. 1"}, SeriesID: 3}
	for _, c := range cases {
		t.Run(c.impl, func(t *testing.T) {
			impl, ok := modules.Lookup(modules.KindNotify, c.impl)
			if !ok {
				t.Fatal("not registered")
			}
			s, err := modules.DecodeSettings(impl, c.settings)
			if err != nil {
				t.Fatal(err)
			}
			inst, err := impl.New(modules.Deps{HTTP: srv.Client()}, s)
			if err != nil {
				t.Fatal(err)
			}
			if err := inst.(notify.Module).Send(context.Background(), msg); err != nil {
				t.Fatal(err)
			}
			c.check(t, got)
		})
	}
}

func TestSecretsMasked(t *testing.T) {
	impl, _ := modules.Lookup(modules.KindNotify, "telegram")
	masked := modules.MaskSecrets(impl, map[string]any{"botToken": "secret", "chatId": "1"})
	if masked["botToken"] != modules.SecretMask || masked["chatId"] != "1" {
		t.Fatalf("masking: %v", masked)
	}
	merged := modules.MergeSecrets(impl, map[string]any{"botToken": modules.SecretMask, "chatId": "2"}, map[string]any{"botToken": "secret"})
	if merged["botToken"] != "secret" {
		t.Fatalf("merge: %v", merged)
	}
}
