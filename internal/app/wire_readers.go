package app

import "context"

// ReaderServices are created by wireReaders.
type ReaderServices struct{}

// wireReaders registers read-progress sync, cleanup and upscaling.
func (a *App) wireReaders(ctx context.Context) error { return nil }
