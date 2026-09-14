// Package app wires every service together. It is the composition root used
// by cmd/mangarr and by integration tests.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Asion001/mangarr/internal/auth"
	"github.com/Asion001/mangarr/internal/catalogs"
	"github.com/Asion001/mangarr/internal/config"
	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/envcfg"
	"github.com/Asion001/mangarr/internal/events"
	"github.com/Asion001/mangarr/internal/imageenc"
	"github.com/Asion001/mangarr/internal/jobs"
	"github.com/Asion001/mangarr/internal/logging"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/processing"
	"github.com/Asion001/mangarr/internal/settings"
	"github.com/Asion001/mangarr/internal/sourcecache"
)

type App struct {
	Cfg      *config.Config
	Log      *slog.Logger
	LogRing  *logging.Ring
	DB       *db.DB
	Settings *settings.Store
	Bus      *events.Bus
	Auth     *auth.Service
	Modules  *modules.Manager
	Catalogs *catalogs.Service
	// SourceCache caches catalog responses (keys include the catalogs generation).
	SourceCache *sourcecache.Cache
	// Encoder re-encodes pages (set before New to override engine detection in tests).
	Encoder    *imageenc.Encoder
	Processing *processing.Processor
	Queue      *jobs.Queue
	Scheduler  *jobs.Scheduler
	HTTP       *http.Client
	StartedAt  time.Time
	Services
	MoreServices
	ReaderServices

	services []Service
}

// Service is a long-running component started with the app.
type Service interface {
	Start(ctx context.Context) error
}

func New(ctx context.Context, cfg *config.Config, log *slog.Logger, ring *logging.Ring) (*App, error) {
	d, err := db.Open(ctx, cfg.DB)
	if err != nil {
		return nil, err
	}
	if err := d.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	st := settings.NewStore(d)
	if err := envcfg.ApplySettings(cfg.Env, st); err != nil {
		return nil, fmt.Errorf("environment: %w", err)
	}
	if err := envcfg.SyncRootFolders(ctx, d, cfg.Env); err != nil {
		return nil, fmt.Errorf("environment: %w", err)
	}
	if unknown := envcfg.Unknown(cfg.Env); len(unknown) > 0 {
		log.Warn("ignoring unknown MANGARR_ variables", "vars", unknown)
	}
	a := &App{
		Cfg: cfg, Log: log, LogRing: ring, DB: d,
		Settings:  st,
		Bus:       events.NewBus(),
		HTTP:      &http.Client{Timeout: 5 * time.Minute},
		StartedAt: time.Now().UTC(),
	}
	if err := a.Settings.Warm(ctx); err != nil {
		return nil, err
	}
	if _, err := a.Settings.EnsureSecrets(ctx); err != nil {
		return nil, err
	}
	if err := a.ensureDefaults(ctx); err != nil {
		return nil, err
	}
	a.Auth = auth.NewService(d, a.Settings, cfg.AuthDisabled)
	a.Modules = modules.NewManager(d, a.HTTP, log, cfg.DataDir)
	a.Catalogs = catalogs.New(d, a.Modules, a.Bus, a.Settings, log.With("component", "catalogs"))
	a.SourceCache = sourcecache.New(32 << 20)
	if err := a.Catalogs.Load(ctx); err != nil {
		return nil, err
	}
	if err := envcfg.SyncModules(ctx, d, a.Modules, cfg.Env); err != nil {
		return nil, fmt.Errorf("environment: %w", err)
	}
	a.Queue = jobs.NewQueue(d, a.Bus, log.With("component", "commands"), 3)
	a.Scheduler = jobs.NewScheduler(d, a.Queue, log.With("component", "scheduler"))
	if err := a.Modules.Reload(ctx); err != nil {
		return nil, err
	}
	a.Modules.OnChange(func() { a.Bus.Changed("module", "sync", 0) })
	if err := a.wire(ctx); err != nil {
		return nil, err
	}
	return a, nil
}

// AddService registers a component started by Start.
func (a *App) AddService(s Service) { a.services = append(a.services, s) }

// Start launches background workers. It returns once they are running.
func (a *App) Start(ctx context.Context) error {
	if err := a.Queue.Start(ctx); err != nil {
		return err
	}
	for _, s := range a.services {
		if err := s.Start(ctx); err != nil {
			return err
		}
	}
	go a.Scheduler.Run(ctx)
	return nil
}

func (a *App) Close() error { return a.DB.Close() }

// ensureDefaults creates the default profile on first start.
func (a *App) ensureDefaults(ctx context.Context) error {
	var existing []model.Profile
	if err := a.DB.NewSelect().Model(&existing).Scan(ctx); err != nil {
		return err
	}
	// profiles saved before re-encoding existed get its defaults
	for _, p := range existing {
		if p.Config.Encode.Format != "" {
			continue
		}
		d := DefaultProfileConfig()
		p.Config.Encode, p.Config.ProcessTiming = d.Encode, d.ProcessTiming
		if _, err := a.DB.NewUpdate().Model(&p).Column("config").WherePK().Exec(ctx); err != nil {
			return err
		}
	}
	if len(existing) > 0 {
		return nil
	}
	now := time.Now().UTC()
	p := &model.Profile{Name: "Default", IsDefault: true, CreatedAt: now, UpdatedAt: now, Config: DefaultProfileConfig()}
	_, err := a.DB.NewInsert().Model(p).Exec(ctx)
	return err
}

func DefaultProfileConfig() model.ProfileConfig {
	return model.ProfileConfig{
		PreferredScanlators: []string{},
		BlockedScanlators:   []string{},
		AllowUpgrades:       false,
		Upscale: model.UpscaleConfig{
			Enabled: false, MinWidth: 1400, MaxWidth: 2048, Model: "waifu2x-cunet", Noise: 1, Format: "webp", Quality: 90,
		},
		Encode:        model.EncodeConfig{Format: "keep", Preset: "balanced", Grayscale: true, MinSavingsPct: 10, RecycleOriginals: true},
		ProcessTiming: "background",
	}
}
