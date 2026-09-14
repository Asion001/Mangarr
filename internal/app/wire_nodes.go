package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Asion001/mangarr/internal/config"
	"github.com/Asion001/mangarr/internal/health"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/upscaler"
)

// NodeOnlineAfter is how long a processing node counts as online after its
// last heartbeat.
const NodeOnlineAfter = 3*upscaler.HeartbeatInterval + 10*time.Second

// NodeStatus is a self-registered processing node.
type NodeStatus struct {
	NodeID   string        `json:"nodeId"`
	Name     string        `json:"name"`
	URL      string        `json:"url"`
	ModuleID int64         `json:"moduleId"`
	LastSeen time.Time     `json:"lastSeen"`
	Online   bool          `json:"online"`
	Info     upscaler.Info `json:"info"`
}

// Nodes tracks processing nodes that register themselves.
type Nodes struct {
	mu    sync.Mutex
	nodes map[string]*NodeStatus
}

func managedByNode(id string) string { return "node:" + id }

// Online reports whether a module instance is reachable: node-managed
// instances only while their node sends heartbeats.
func (n *Nodes) Online(def model.ProviderDefinition) bool {
	id, ok := strings.CutPrefix(def.ManagedBy, "node:")
	if !ok {
		return true
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	s := n.nodes[id]
	return s != nil && time.Since(s.LastSeen) < NodeOnlineAfter
}

// List returns the known nodes.
func (n *Nodes) List() []NodeStatus {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]NodeStatus, 0, len(n.nodes))
	for _, s := range n.nodes {
		c := *s
		c.Online = time.Since(c.LastSeen) < NodeOnlineAfter
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Heartbeat records a node and keeps its upscaler module up to date.
func (a *App) Heartbeat(ctx context.Context, hb upscaler.Heartbeat) (NodeStatus, error) {
	if hb.NodeID == "" || hb.URL == "" {
		return NodeStatus{}, fmt.Errorf("nodeId and url are required")
	}
	managed := managedByNode(hb.NodeID)
	settings := map[string]any{"url": hb.URL, "token": hb.Token}
	var def model.ProviderDefinition
	err := a.DB.NewSelect().Model(&def).Where("managed_by = ?", managed).Limit(1).Scan(ctx)
	switch {
	case err != nil:
		def = model.ProviderDefinition{Kind: string(modules.KindUpscale), Implementation: "ncnn-worker", Name: hb.Name + " (node)",
			Enabled: true, Priority: 10, Settings: settings}
		if err := a.Modules.Create(ctx, &def); err != nil {
			return NodeStatus{}, err
		}
		if _, err := a.DB.NewUpdate().Model(&def).Set("managed_by = ?", managed).WherePK().Exec(ctx); err != nil {
			return NodeStatus{}, err
		}
		_ = a.Modules.Reload(ctx)
		a.Log.Info("processing node registered", "node", hb.Name, "url", hb.URL)
	case def.Settings["url"] != hb.URL || def.Settings["token"] != hb.Token:
		def.Settings["url"], def.Settings["token"] = hb.URL, hb.Token
		if err := a.Modules.Update(ctx, &def); err != nil {
			return NodeStatus{}, err
		}
	}
	a.Nodes.mu.Lock()
	prev := a.Nodes.nodes[hb.NodeID]
	wasOnline := prev != nil && time.Since(prev.LastSeen) < NodeOnlineAfter
	st := &NodeStatus{NodeID: hb.NodeID, Name: hb.Name, URL: hb.URL, ModuleID: def.ID, LastSeen: time.Now().UTC(), Online: true, Info: hb.Info}
	a.Nodes.nodes[hb.NodeID] = st
	a.Nodes.mu.Unlock()
	if !wasOnline {
		a.Bus.Changed("module", "node-online", def.ID)
		a.PushProcessBacklog("node-online") // chapters waiting for an upscaler
	}
	return *st, nil
}

// wireNodes sets up node tracking and, in integrated mode, the built-in upscaler.
func (a *App) wireNodes(ctx context.Context) error {
	a.Nodes = &Nodes{nodes: map[string]*NodeStatus{}}
	a.Processing.Up.Online = a.Nodes.Online
	a.Health.AddCheck(func(ctx context.Context) []health.Check {
		var out []health.Check
		for _, n := range a.Nodes.List() {
			if !n.Online {
				out = append(out, health.Check{Source: "Processing nodes", Type: health.Notice, Link: "/settings/upscalers",
					Message: fmt.Sprintf("%s is offline since %s; chapters wait until it's back", n.Name, n.LastSeen.Local().Format("Jan 2 15:04"))})
			}
		}
		return out
	})
	if a.Cfg.Mode == config.ModeIntegrated {
		return a.offerLocalUpscaler(ctx)
	}
	return nil
}

// localOfferedKey remembers that the built-in upscaler was set up once (so
// deleting it in the UI sticks).
const localOfferedKey = "local_upscaler_offered"

// offerLocalUpscaler adds the built-in upscaler when the image has the tools.
func (a *App) offerLocalUpscaler(ctx context.Context) error {
	var offered bool
	_ = a.Settings.Get(ctx, localOfferedKey, &offered)
	dir := "/opt/upscalers"
	if offered || !upscaler.ToolsAvailable(dir) {
		return nil
	}
	// only enabled by default with a real GPU (lavapipe on the CPU is very slow)
	gpu := false
	for _, d := range upscaler.Devices() {
		if !strings.Contains(strings.ToLower(d), "llvmpipe") && !strings.Contains(strings.ToLower(d), "lavapipe") {
			gpu = true
		}
	}
	def := &model.ProviderDefinition{Kind: string(modules.KindUpscale), Implementation: "local", Name: "Built-in (this server)",
		Enabled: gpu, Priority: 50, Settings: map[string]any{"toolsDir": dir, "gpu": "auto"}}
	if err := a.Modules.Create(ctx, def); err != nil {
		return err
	}
	a.Log.Info("added the built-in upscaler", "gpu", gpu)
	return a.Settings.Set(ctx, localOfferedKey, true)
}
