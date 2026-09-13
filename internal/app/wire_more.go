package app

import "context"

// wireMore registers services added in later milestones (downloads manager,
// notifications, health, readers/cleanup, upscaling, backups).
func (a *App) wireMore(ctx context.Context) error { return nil }
