package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/redact"
)

// secretKey reports whether a settings key looks like a credential.
func secretKey(k string) bool {
	k = strings.ToLower(k)
	for _, s := range []string{"password", "passwd", "token", "secret", "apikey", "api_key", "cookie"} {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// Secrets lists the credential values mangarr currently knows: its API key
// and session secret, secret module settings and reader account credentials.
func (a *App) Secrets(ctx context.Context) []string {
	var out []string
	if g, err := a.Settings.General(ctx); err == nil {
		out = append(out, g.APIKey, g.SessionSecret)
	}
	var defs []model.ProviderDefinition
	_ = a.DB.NewSelect().Model(&defs).Scan(ctx)
	for _, d := range defs {
		secret := map[string]bool{}
		if impl, ok := modules.Lookup(modules.Kind(d.Kind), d.Implementation); ok {
			for _, f := range modules.FieldsOf(impl.Settings()) {
				if f.Secret {
					secret[f.Name] = true
				}
			}
		}
		for k, v := range d.Settings {
			if secret[k] || secretKey(k) {
				out = append(out, fmt.Sprint(v))
			}
		}
	}
	var accounts []model.ReaderAccount
	_ = a.DB.NewSelect().Model(&accounts).Scan(ctx)
	for _, acc := range accounts {
		for _, v := range acc.Credentials {
			out = append(out, v)
		}
	}
	return out
}

// Redactor masks the current secrets and credential-looking text.
func (a *App) Redactor(ctx context.Context) *redact.Redactor { return redact.New(a.Secrets(ctx)) }
