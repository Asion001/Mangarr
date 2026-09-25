package api

import (
	"context"
	"sync"
	"time"

	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/mediaserver"
)

type WatchLink struct {
	ServerName string `json:"serverName"`
	Kind       string `json:"kind" enum:"jellyfin,silo"`
	URL        string `json:"url"`
}

type AdaptationResource struct {
	model.Adaptation
	WatchLinks []WatchLink `json:"watchLinks" nullable:"false"`
}

// adaptationWatchLinks adds links from the configured media servers. The
// lookups run detached from the request so a slow server (a first catalog
// load, say) still fills its cache; the page waits for them only briefly and
// shows what is ready.
func (s *Server) adaptationWatchLinks(ctx context.Context, adaptations []AdaptationResource) {
	if len(adaptations) == 0 {
		return
	}
	servers := modules.ActiveAs[mediaserver.Module](s.app.Modules, modules.KindMediaServer)
	if len(servers) == 0 {
		return
	}
	var mu sync.Mutex
	results := make([][]string, len(servers))
	for i := range servers {
		results[i] = make([]string, len(adaptations))
	}
	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	var wg sync.WaitGroup
	for i, server := range servers {
		for j, a := range adaptations {
			wg.Go(func() {
				if link, err := server.Instance.Find(bg, a.Adaptation); err == nil && link != "" {
					mu.Lock()
					results[i][j] = link
					mu.Unlock()
				}
			})
		}
	}
	done := make(chan struct{})
	go func() { wg.Wait(); cancel(); close(done) }()
	wait, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	select {
	case <-done:
	case <-wait.Done():
	}
	mu.Lock()
	defer mu.Unlock()
	// Results keep the servers' configured priority order.
	for i, server := range servers {
		for j, link := range results[i] {
			if link != "" {
				adaptations[j].WatchLinks = append(adaptations[j].WatchLinks, WatchLink{
					ServerName: server.Def.Name, Kind: server.Def.Implementation, URL: link,
				})
			}
		}
	}
}
