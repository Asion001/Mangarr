package api_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Asion001/mangarr/internal/app"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/testutil/fakeupscaler"
	"github.com/Asion001/mangarr/internal/upscaler"
)

func TestNodeSelfRegistration(t *testing.T) {
	srv, a := newServer(t, true)
	worker := upscaler.NewServer(upscaler.Config{Token: "tok", TmpDir: t.TempDir(), Version: "test"}, fakeupscaler.Runner{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ws := httptest.NewServer(worker.Handler())
	defer ws.Close()

	hb := upscaler.Heartbeat{NodeID: "n1", Name: "gaming-pc", URL: ws.URL, Token: "tok", Info: worker.Info()}
	body, _ := json.Marshal(hb)
	var st app.NodeStatus
	if code := doJSON(t, http.MethodPost, srv.URL+"/api/v1/upscaler-nodes/heartbeat", string(body), &st); code != 200 || !st.Online || st.ModuleID == 0 {
		t.Fatalf("heartbeat: %d %+v", code, st)
	}
	l, ok := a.Modules.Get(st.ModuleID)
	if !ok || l.Def.ManagedBy != "node:n1" || l.Def.Settings["url"] != ws.URL || !a.Nodes.Online(l.Def) {
		t.Fatalf("node module: %+v", l)
	}
	// a second heartbeat with a new address updates the same module
	hb.URL = ws.URL + "/"
	body, _ = json.Marshal(hb)
	doJSON(t, http.MethodPost, srv.URL+"/api/v1/upscaler-nodes/heartbeat", string(body), &st)
	var defs []model.ProviderDefinition
	_ = a.DB.NewSelect().Model(&defs).Where("kind = ?", "upscale").Scan(t.Context())
	if len(defs) != 1 || defs[0].Settings["url"] != hb.URL {
		t.Fatalf("want one updated node module, got %+v", defs)
	}
	var nodes []app.NodeStatus
	doJSON(t, http.MethodGet, srv.URL+"/api/v1/upscaler-nodes", "", &nodes)
	if len(nodes) != 1 || nodes[0].Name != "gaming-pc" {
		t.Fatalf("nodes: %+v", nodes)
	}
	// unknown nodes (not seen since start) count as offline
	if a.Nodes.Online(model.ProviderDefinition{ManagedBy: "node:other"}) {
		t.Fatal("never-seen node must be offline")
	}
	// node modules can be deleted (they re-register while running)
	if err := a.Modules.Delete(t.Context(), st.ModuleID); err != nil {
		t.Fatal(err)
	}
}
