package app

import (
	"context"

	"github.com/Asion001/mangarr/internal/komgaapi"
	"github.com/Asion001/mangarr/internal/reading"
)

// wireReading sets up what reading apps see and the Komga-compatible API.
func (a *App) wireReading(ctx context.Context) error {
	a.Reading = &reading.Service{DB: a.DB, Settings: a.Settings, Library: a.Library, ImageCache: a.ImageCache, Mods: a.Modules,
		HTTP: a.HTTP, Bus: a.Bus, Downloads: a.Searcher, Log: a.Log.With("component", "reading")}
	a.Komga = komgaapi.NewService(komgaapi.Deps{DB: a.DB, Settings: a.Settings, Auth: a.Auth, Reading: a.Reading, Bus: a.Bus,
		Log: a.Log.With("component", "komga-api")}, a.Cfg.KomgaListen)
	a.AddService(a.Komga)
	return nil
}
