package api_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/api"
	"github.com/Asion001/mangarr/internal/app"
	"github.com/Asion001/mangarr/internal/config"
	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/logging"
	"github.com/Asion001/mangarr/internal/model"
)

func TestSeriesAdaptationsRefreshAndAPI(t *testing.T) {
	fixture, err := os.ReadFile("../modules/metadata/anilist/testdata/adaptations.json")
	if err != nil {
		t.Fatal(err)
	}
	for dialect, dsn := range dbtest.DSNs(t) {
		t.Run(dialect, func(t *testing.T) {
			ctx := context.Background()
			_, ring := logging.Setup("error", io.Discard)
			a, err := app.New(ctx, &config.Config{DataDir: t.TempDir(), DB: dsn, AuthDisabled: true}, slog.New(slog.NewTextHandler(io.Discard, nil)), ring)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = a.Close() })
			srv := httptest.NewServer(api.New(a))
			defer srv.Close()
			var phase atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch phase.Load() {
				case 0:
					_, _ = w.Write(append(append([]byte(`{"data":{"Media":`), fixture...), []byte(`}}`)...))
				case 1:
					_, _ = io.WriteString(w, `{"data":{"Media":{"id":700001,"idMal":800001,"title":{"english":"Cloud Lantern"},"relations":{"edges":[{"relationType":"ADAPTATION","node":{"id":700002,"type":"ANIME","format":"MOVIE","title":{"english":"Cloud Lantern Revised"},"startDate":{"year":2024}}}]}}}}`)
				case 2:
					_, _ = io.WriteString(w, `{"errors":[{"message":"temporarily unavailable","status":503}]}`)
				case 3:
					_, _ = io.WriteString(w, `{"data":{"Media":{"id":700001,"idMal":800001,"title":{"english":"Cloud Lantern"},"relations":{"edges":[]}}}}`)
				}
			}))
			defer upstream.Close()
			def := &model.ProviderDefinition{Kind: "metadata", Implementation: "anilist", Name: "Test metadata", Enabled: true,
				Settings: map[string]any{"endpoint": upstream.URL, "titleLanguage": "english"}}
			if err := a.Modules.Create(ctx, def); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			root := &model.RootFolder{Path: t.TempDir(), Language: "en", CreatedAt: now}
			if _, err := a.DB.NewInsert().Model(root).Exec(ctx); err != nil {
				t.Fatal(err)
			}
			var profile model.Profile
			if err := a.DB.NewSelect().Model(&profile).Order("id").Limit(1).Scan(ctx); err != nil {
				t.Fatal(err)
			}
			ser := &model.Series{Title: "Cloud Lantern", SortTitle: "cloud lantern", Status: model.StatusOngoing,
				RootFolderID: root.ID, ProfileID: profile.ID, Path: "Cloud Lantern", Language: "en", Tags: []int64{},
				AddedAt: now, UpdatedAt: now}
			if _, err := a.DB.NewInsert().Model(ser).Exec(ctx); err != nil {
				t.Fatal(err)
			}
			detail := func() api.SeriesResource {
				t.Helper()
				var got api.SeriesResource
				if code := doJSON(t, http.MethodGet, srv.URL+"/api/v1/series/"+itoa(ser.ID), "", &got); code != 200 {
					t.Fatalf("detail: %d", code)
				}
				if got.Adaptations == nil {
					t.Fatal("adaptations must be an array, not null or omitted")
				}
				return got
			}
			if len(detail().Adaptations) != 0 {
				t.Fatal("series without AniList should have no adaptations")
			}
			ser.Metadata.ExternalIDs = map[string]string{"anilist": "700001"}
			if _, err := a.DB.NewUpdate().Model(ser).Column("metadata").WherePK().Exec(ctx); err != nil {
				t.Fatal(err)
			}
			if changed, err := a.Series.RefreshMetadata(ctx, ser.ID); err != nil || !changed {
				t.Fatalf("initial refresh: %v %v", changed, err)
			}
			got := detail()
			if len(got.Adaptations) != 6 || !reflect.DeepEqual(got.Adaptations, got.Metadata.Adaptations) {
				t.Fatalf("persisted adaptations: %+v", got.Adaptations)
			}
			if got.LastMetadataRefresh == nil {
				t.Fatal("missing refresh timestamp")
			}
			if changed, err := a.Series.RefreshMetadata(ctx, ser.ID); err != nil || changed {
				t.Fatalf("unchanged refresh: %v %v", changed, err)
			}
			phase.Store(1)
			if changed, err := a.Series.RefreshMetadata(ctx, ser.ID); err != nil || !changed {
				t.Fatalf("updated refresh: %v %v", changed, err)
			}
			got = detail()
			if len(got.Adaptations) != 1 || got.Adaptations[0].Title != "Cloud Lantern Revised" || got.Adaptations[0].Year != 2024 || got.Adaptations[0].Format != "movie" {
				t.Fatalf("refresh: %+v", got.Adaptations)
			}
			phase.Store(2)
			if _, err := a.Series.RefreshMetadata(ctx, ser.ID); err == nil {
				t.Fatal("expected failed refresh")
			}
			if !reflect.DeepEqual(detail().Adaptations, got.Adaptations) {
				t.Fatal("failed refresh changed adaptations")
			}
			phase.Store(3)
			if changed, err := a.Series.RefreshMetadata(ctx, ser.ID); err != nil || !changed {
				t.Fatalf("empty refresh: %v %v", changed, err)
			}
			if len(detail().Adaptations) != 0 {
				t.Fatal("removed adaptations retained")
			}
		})
	}
}
