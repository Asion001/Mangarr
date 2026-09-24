package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/api"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
)

func TestUpscalerModelCatalogKeepsOfflineWorkerModels(t *testing.T) {
	srv, app := newServer(t, false)
	ctx := context.Background()
	pool := &model.ProviderDefinition{Kind: string(modules.KindUpscale), Implementation: "workers", Name: "Workers", Enabled: true,
		Priority: 20, Settings: map[string]any{}, Tags: []int64{}, Events: []string{}}
	if err := app.Modules.Create(ctx, pool); err != nil {
		t.Fatal(err)
	}
	tools := t.TempDir()
	waifu2x := filepath.Join(tools, "waifu2x")
	if err := os.MkdirAll(filepath.Join(waifu2x, "models-cunet"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(waifu2x, "waifu2x-ncnn-vulkan"), []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}
	local := &model.ProviderDefinition{Kind: string(modules.KindUpscale), Implementation: "local", Name: "Built-in GPU", Enabled: true,
		Priority: 10, Settings: map[string]any{"toolsDir": tools}, Tags: []int64{}, Events: []string{}}
	if err := app.Modules.Create(ctx, local); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	models := func(names ...string) []map[string]any {
		out := make([]map[string]any, 0, len(names))
		for _, name := range names {
			out = append(out, map[string]any{"name": name, "description": name + " description", "scales": []int{2, 4}})
		}
		return out
	}
	for _, worker := range []*model.Worker{
		{Name: "Online GPU", Prefix: "mgw_online", KeyHash: "hash-1", Roles: []string{model.RoleUpscale}, Enabled: true, Priority: 10,
			Info: map[string]any{"models": models("shared", "online-only"), "devices": []string{"RTX"}}, CreatedAt: now, LastSeenAt: &now},
		{Name: "Remembered GPU", Prefix: "mgw_offline", KeyHash: "hash-2", Roles: []string{model.RoleUpscale}, Enabled: true, Priority: 20,
			Info: map[string]any{"models": models("shared", "offline-only"), "devices": []string{"Arc"}}, CreatedAt: now},
		{Name: "Disabled GPU", Prefix: "mgw_disabled", KeyHash: "hash-3", Roles: []string{model.RoleUpscale}, Enabled: false, Priority: 30,
			Info: map[string]any{"models": models("disabled-only")}, CreatedAt: now},
	} {
		if _, err := app.DB.NewInsert().Model(worker).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	general, _ := app.Settings.General(ctx)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/upscalers/models", nil)
	req.Header.Set("X-Api-Key", general.APIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var catalog api.UpscalerModelCatalog
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Models) != 4 {
		t.Fatalf("models %+v", catalog.Models)
	}
	byName := map[string]api.UpscalerModel{}
	for _, mdl := range catalog.Models {
		byName[mdl.Name] = mdl
	}
	if locations := byName["shared"].Sources; len(locations) != 2 || !locations[0].Available || locations[1].Available {
		t.Fatalf("shared locations %+v", locations)
	}
	if locations := byName["offline-only"].Sources; len(locations) != 1 || locations[0].Name != "Remembered GPU" || locations[0].Available {
		t.Fatalf("offline model %+v", locations)
	}
	if _, exists := byName["disabled-only"]; exists {
		t.Fatalf("disabled worker model leaked into catalog: %+v", catalog.Models)
	}
	if locations := byName["waifu2x-cunet"].Sources; len(locations) != 1 || locations[0].Name != "Built-in GPU" || !locations[0].Available {
		t.Fatalf("built-in model %+v", locations)
	}
}
