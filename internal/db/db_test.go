package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/model"
)

func TestMigrateAndRoundTrip(t *testing.T) {
	dbtest.ForEachDialect(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Millisecond)

		rf := &model.RootFolder{Path: "/data/manga/en", Language: "en", CreatedAt: now}
		if _, err := d.NewInsert().Model(rf).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		p := &model.Profile{Name: "Default", IsDefault: true, CreatedAt: now, UpdatedAt: now,
			Config: model.ProfileConfig{PreferredScanlators: []string{"^Official$"}, Upscale: model.UpscaleConfig{MinWidth: 1400}}}
		if _, err := d.NewInsert().Model(p).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		s := &model.Series{Title: "One Piece", SortTitle: "one piece", Status: model.StatusOngoing, Monitored: true,
			MonitorNew: "all", RootFolderID: rf.ID, Path: "One Piece", ProfileID: p.ID, Tags: []int64{1, 2},
			Metadata: model.SeriesMetadata{Genres: []string{"Action"}, ExternalIDs: map[string]string{"anilist": "30013"}},
			AddedAt: now, UpdatedAt: now}
		if _, err := d.NewInsert().Model(s).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		var got model.Series
		if err := d.NewSelect().Model(&got).Where("id = ?", s.ID).Scan(ctx); err != nil {
			t.Fatal(err)
		}
		if got.Title != "One Piece" || !got.Monitored || len(got.Tags) != 2 || got.Metadata.ExternalIDs["anilist"] != "30013" {
			t.Fatalf("round trip mismatch: %+v", got)
		}
		if !got.AddedAt.Equal(now) {
			t.Fatalf("time mismatch: %v vs %v", got.AddedAt, now)
		}
		var gp model.Profile
		if err := d.NewSelect().Model(&gp).Where("id = ?", p.ID).Scan(ctx); err != nil {
			t.Fatal(err)
		}
		if gp.Config.Upscale.MinWidth != 1400 || gp.Config.PreferredScanlators[0] != "^Official$" {
			t.Fatalf("profile config mismatch: %+v", gp.Config)
		}
		// timestamp comparison in SQL must work on both dialects
		n, err := d.NewSelect().Model((*model.Series)(nil)).Where("added_at <= ?", now.Add(time.Second)).Count(ctx)
		if err != nil || n != 1 {
			t.Fatalf("time comparison: n=%d err=%v", n, err)
		}
	})
}
