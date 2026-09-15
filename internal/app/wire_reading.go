package app

import (
	"context"

	"github.com/Asion001/mangarr/internal/komgaapi"
)

// wireReading sets up the Komga-compatible API reading apps use.
func (a *App) wireReading(ctx context.Context) error {
	a.Komga = komgaapi.NewService(komgaapi.Deps{DB: a.DB, Settings: a.Settings, Auth: a.Auth, Log: a.Log.With("component", "komga-api")},
		a.Cfg.KomgaListen)
	a.AddService(a.Komga)
	return nil
}
