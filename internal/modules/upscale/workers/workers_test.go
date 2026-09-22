package workers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/worktasks"
)

func TestHealthCheckIgnoresDisabledWorkers(t *testing.T) {
	dbtest.ForEachDialect(t, func(t *testing.T, d *db.DB) {
		previous := worktasks.Default()
		worktasks.SetDefault(worktasks.New(d, slog.New(slog.NewTextHandler(io.Discard, nil))))
		t.Cleanup(func() { worktasks.SetDefault(previous) })
		m := &Module{log: slog.Default(), timeout: time.Minute}
		if _, err := m.HealthCheck(context.Background()); !errors.Is(err, ErrNoWorker) {
			t.Fatalf("missing worker should still fail health check, got %v", err)
		}

		worker := &model.Worker{Name: "disabled GPU", KeyHash: "hash", Prefix: "mgw_test", Roles: []string{model.RoleUpscale},
			Enabled: false, Info: map[string]any{}, CreatedAt: time.Now().UTC()}
		if _, err := d.NewInsert().Model(worker).Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		if warning, err := m.HealthCheck(context.Background()); err != nil || warning != "" {
			t.Fatalf("disabled worker caused a health issue: warning=%q err=%v", warning, err)
		}

		if _, err := d.NewUpdate().Model(worker).Set("enabled = ?", true).WherePK().Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := m.HealthCheck(context.Background()); !errors.Is(err, ErrNoWorker) {
			t.Fatalf("enabled offline worker should still fail health check, got %v", err)
		}
	})
}
