package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/upscale"
	"github.com/Asion001/mangarr/internal/naming"
)

func init() { register((*Server).registerExtras) }

type NamingPreview struct {
	Folder   string `json:"folder"`
	Chapter  string `json:"chapter"`
	Decimal  string `json:"decimal"`
	Volume   string `json:"volume"`
	Examples string `json:"examples"`
}

func (s *Server) registerExtras() {
	huma.Register(s.api, huma.Operation{OperationID: "naming-preview", Method: http.MethodGet, Path: "/api/v1/settings/media/preview", Tags: []string{"Settings"},
		Summary: "Render naming templates with sample values"},
		func(ctx context.Context, in *struct {
			ChapterFormat string `query:"chapterFormat"`
			FolderFormat  string `query:"folderFormat"`
		}) (*struct{ Body NamingPreview }, error) {
			v := naming.Values{SeriesTitle: "Re:Zero - Starting Life in Another World", SeriesYear: 2014, Chapter: 12, HasChapter: true,
				ChapterTitle: "The Witch's Scent", Scanlator: "Official", Source: "MangaDex (EN)", Language: "en"}
			out := NamingPreview{Folder: naming.Render(in.FolderFormat, v), Chapter: naming.Render(in.ChapterFormat, v) + ".cbz"}
			v.Chapter = 104.5
			out.Decimal = naming.Render(in.ChapterFormat, v) + ".cbz"
			v.Volume = "3"
			out.Volume = naming.Render(in.ChapterFormat, v) + ".cbz"
			return &struct{ Body NamingPreview }{out}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "upscaler-info", Method: http.MethodGet, Path: "/api/v1/modules/{id}/upscaler-info", Tags: []string{"Modules"},
		Summary: "Models and devices of an upscaler module"},
		func(ctx context.Context, in *IDPath) (*struct{ Body *upscale.Info }, error) {
			up, _, err := modules.GetAs[upscale.Module](s.app.Modules, in.ID)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			info, err := up.Info(ctx)
			if err != nil {
				return nil, huma.Error502BadGateway(err.Error())
			}
			return &struct{ Body *upscale.Info }{info}, nil
		})
}
