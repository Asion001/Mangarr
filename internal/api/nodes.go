package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/app"
	"github.com/Asion001/mangarr/internal/upscaler"
)

func init() { register((*Server).registerNodes) }

func (s *Server) registerNodes() {
	tags := []string{"Processing"}
	huma.Register(s.api, huma.Operation{OperationID: "upscaler-nodes-heartbeat", Method: http.MethodPost, Path: "/api/v1/upscaler-nodes/heartbeat", Tags: tags,
		Summary: "Processing nodes (MANGARR_MODE=upscaler with MANGARR_SERVER_URL) register themselves here every 30 s"},
		func(ctx context.Context, in *struct{ Body upscaler.Heartbeat }) (*struct{ Body app.NodeStatus }, error) {
			st, err := s.app.Heartbeat(ctx, in.Body)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return &struct{ Body app.NodeStatus }{st}, nil
		})
	huma.Register(s.api, huma.Operation{OperationID: "upscaler-nodes-list", Method: http.MethodGet, Path: "/api/v1/upscaler-nodes", Tags: tags,
		Summary: "Self-registered processing nodes and whether they're online"},
		func(ctx context.Context, _ *struct{}) (*struct{ Body []app.NodeStatus }, error) {
			return &struct{ Body []app.NodeStatus }{s.app.Nodes.List()}, nil
		})
}
