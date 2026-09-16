package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/series"
)

func init() { register((*Server).registerSeriesSources) }

// BulkSourcesOutput is a preview, or the command doing the work.
type BulkSourcesOutput struct {
	// Results is what would happen (a preview), at most PreviewLimit series.
	Results []series.BulkResult `json:"results,omitempty"`
	// Previewed is how many series the preview looked at, of Total.
	Previewed int `json:"previewed,omitempty"`
	Total     int `json:"total"`
	// Command is the queued command when this wasn't a preview.
	Command *model.Command `json:"command,omitempty"`
}

func (s *Server) registerSeriesSources() {
	tags := []string{"Series"}
	huma.Register(s.api, huma.Operation{OperationID: "series-sources-bulk", Method: http.MethodPost, Path: "/api/v1/series/sources/bulk", Tags: tags,
		Summary: "Add a catalog to many series as a fallback source, or remove or switch one off across them"},
		func(ctx context.Context, in *struct {
			Body struct {
				series.BulkRequest
				// DryRun reports what would happen without changing anything.
				DryRun bool `json:"dryRun,omitempty"`
			}
		}) (*struct{ Body BulkSourcesOutput }, error) {
			req := in.Body.BulkRequest
			total, err := s.app.Series.BulkCount(ctx, req)
			if err != nil {
				return nil, seriesError(err)
			}
			out := BulkSourcesOutput{Total: total}
			if in.Body.DryRun {
				results, err := s.app.Series.BulkSources(ctx, req, true, nil)
				if err != nil {
					return nil, seriesError(err)
				}
				out.Results, out.Previewed = results, len(results)
				return &struct{ Body BulkSourcesOutput }{out}, nil
			}
			cmd, err := s.app.Queue.Push(ctx, "SeriesSources", map[string]any{
				"action": req.Action, "moduleId": req.ModuleID, "sourceId": req.SourceID, "seriesIds": req.SeriesIDs,
				"tagId": req.TagID, "rootFolderId": req.RootFolderID, "monitoredOnly": req.MonitoredOnly,
			}, "manual")
			if err != nil {
				return nil, toHTTPError(err)
			}
			out.Command = cmd
			return &struct{ Body BulkSourcesOutput }{out}, nil
		})
}
