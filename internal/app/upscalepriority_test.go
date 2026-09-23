package app_test

import (
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/model"
)

// TestBuiltInUpscalerJoinsTheWorkersOrder: the built-in upscaler used to be
// ordered against the "Workers" module as a whole. Moving it into the
// workers' own priority list keeps the order an install already had.
func TestBuiltInUpscalerJoinsTheWorkersOrder(t *testing.T) {
	cases := []struct {
		name        string
		local, pool int
		want        int
	}{
		{"workers went first", 50, 20, 130},
		{"server went first and stays first", 10, 20, 10},
		{"server went first", 150, 200, 90},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newTestApp(t, dbtest.DSNs(t)["sqlite"])
			local := &model.ProviderDefinition{Kind: "upscale", Implementation: "local", Name: "Built-in", Enabled: true, Priority: c.local,
				Settings: map[string]any{"toolsDir": t.TempDir()}}
			pool := &model.ProviderDefinition{Kind: "upscale", Implementation: "workers", Name: "Workers", Enabled: true, Priority: c.pool,
				Settings: map[string]any{}}
			for _, def := range []*model.ProviderDefinition{local, pool} {
				if err := e.App.Modules.Create(e.Ctx, def); err != nil {
					t.Fatal(err)
				}
			}
			for i, p := range []int{100, 120} {
				w := &model.Worker{Name: []string{"a", "b"}[i], KeyHash: []string{"ha", "hb"}[i], Prefix: "p", Roles: []string{model.RoleUpscale},
					Enabled: true, Priority: p, Info: map[string]any{}, CreatedAt: time.Now().UTC()}
				if _, err := e.App.DB.NewInsert().Model(w).Exec(e.Ctx); err != nil {
					t.Fatal(err)
				}
			}
			if err := e.App.ShareUpscalePriority(e.Ctx); err != nil {
				t.Fatal(err)
			}
			var got model.ProviderDefinition
			if err := e.App.DB.NewSelect().Model(&got).Where("id = ?", local.ID).Scan(e.Ctx); err != nil {
				t.Fatal(err)
			}
			if got.Priority != c.want {
				t.Fatalf("built-in priority = %d, want %d", got.Priority, c.want)
			}
			// once is enough: a later change by hand sticks
			got.Priority = 5
			if err := e.App.Modules.Update(e.Ctx, &got); err != nil {
				t.Fatal(err)
			}
			if err := e.App.ShareUpscalePriority(e.Ctx); err != nil {
				t.Fatal(err)
			}
			_ = e.App.DB.NewSelect().Model(&got).Where("id = ?", local.ID).Scan(e.Ctx)
			if got.Priority != 5 {
				t.Fatalf("the priority was moved again: %d", got.Priority)
			}
		})
	}
}
