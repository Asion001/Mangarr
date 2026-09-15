package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/access"
	"github.com/Asion001/mangarr/internal/model"
)

func init() { register((*Server).registerFollows) }

// follows are the series the caller follows.
func (s *Server) follows(ctx context.Context) map[int64]bool {
	out := map[int64]bool{}
	p := access.From(ctx)
	if p == nil || p.Kind != access.KindUser {
		return out
	}
	var ids []int64
	_ = s.app.DB.NewSelect().Model((*model.Follow)(nil)).Column("series_id").Where("user_id = ?", p.UserID).Scan(ctx, &ids)
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func (s *Server) registerFollows() {
	tags := []string{"Series"}
	follow := func(ctx context.Context, id int64, on bool) error {
		p := access.From(ctx)
		if p == nil || p.Kind != access.KindUser {
			return huma.Error400BadRequest("sign in as a user to follow series")
		}
		if _, err := s.visibleSeries(ctx, id); err != nil {
			return err
		}
		var err error
		if on {
			_, err = s.app.DB.NewInsert().Model(&model.Follow{UserID: p.UserID, SeriesID: id, CreatedAt: time.Now().UTC()}).On("CONFLICT DO NOTHING").Exec(ctx)
		} else {
			_, err = s.app.DB.NewDelete().Model((*model.Follow)(nil)).Where("user_id = ? AND series_id = ?", p.UserID, id).Exec(ctx)
		}
		return toHTTPError(err)
	}
	huma.Register(s.api, huma.Operation{OperationID: "series-follow", Method: http.MethodPut, Path: "/api/v1/series/{id}/follow", Tags: tags,
		Summary: "Follow a series: its new chapters go to your notification targets"},
		func(ctx context.Context, in *IDPath) (*struct{}, error) { return nil, follow(ctx, in.ID, true) })
	huma.Register(s.api, huma.Operation{OperationID: "series-unfollow", Method: http.MethodDelete, Path: "/api/v1/series/{id}/follow", Tags: tags},
		func(ctx context.Context, in *IDPath) (*struct{}, error) { return nil, follow(ctx, in.ID, false) })
}
