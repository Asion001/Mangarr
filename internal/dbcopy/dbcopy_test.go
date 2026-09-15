package dbcopy_test

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/dbcopy"
	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/model"
)

// TestTablesCoverSchema: every table the migrations create is copied.
func TestTablesCoverSchema(t *testing.T) {
	d := dbtest.SQLite(t)
	var names []string
	if err := d.NewSelect().TableExpr("sqlite_master").Column("name").Where("type = 'table'").
		Where("name NOT IN ('sqlite_sequence', 'goose_db_version')").Scan(context.Background(), &names); err != nil {
		t.Fatal(err)
	}
	want := append([]string(nil), dbcopy.Tables...)
	sort.Strings(names)
	sort.Strings(want)
	if len(names) != len(want) {
		t.Fatalf("schema tables %v\ncopied tables %v", names, want)
	}
	for i := range names {
		if names[i] != want[i] {
			t.Fatalf("schema tables %v\ncopied tables %v", names, want)
		}
	}
}

// seed fills a database with rows that exercise the conversions: booleans,
// times (and NULL times), JSON, arrays in JSON, foreign keys.
func seed(t *testing.T, d *db.DB) (*model.Series, time.Time) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 9, 15, 17, 4, 5, 123456000, time.UTC)
	rf := &model.RootFolder{Path: "/data/manga", Language: "en", CreatedAt: now}
	p := &model.Profile{Name: "Default", IsDefault: true, CreatedAt: now, UpdatedAt: now,
		Config: model.ProfileConfig{PreferredScanlators: []string{"^Official$"}, BlockedScanlators: []string{}}}
	tag := &model.Tag{Label: "keep"}
	for _, m := range []any{rf, p, tag} {
		if _, err := d.NewInsert().Model(m).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	ser := &model.Series{Title: "Dr. STONE", SortTitle: "dr. stone", Status: model.StatusOngoing, Monitored: true, MonitorNew: model.MonitorAll,
		RootFolderID: rf.ID, Path: "Dr. STONE", ProfileID: p.ID, Tags: []int64{tag.ID}, AddedAt: now, UpdatedAt: now,
		Metadata: model.SeriesMetadata{AltTitles: []string{"ドクターストーン"}, Genres: []string{"Sci-Fi"}, Links: map[string]string{"AniList": "https://anilist.co/manga/98416"}}}
	if _, err := d.NewInsert().Model(ser).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		c := &model.Chapter{SeriesID: ser.ID, NumberKey: string(rune('0' + i)), NumberSort: float64(i) + 0.5, Monitored: i != 2,
			State: model.ChapterMissing, FirstSeenAt: now, UpdatedAt: now}
		if _, err := d.NewInsert().Model(c).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	r := &model.Reader{Name: "ann", CountForCleanup: true, CreatedAt: now}
	if _, err := d.NewInsert().Model(r).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := d.NewInsert().Model(&model.Setting{Key: "general", Value: `{"instanceName":"mangarr"}`, UpdatedAt: now}).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	return ser, now
}

func check(t *testing.T, d *db.DB, want *model.Series, now time.Time) {
	t.Helper()
	ctx := context.Background()
	var got model.Series
	if err := d.NewSelect().Model(&got).Where("id = ?", want.ID).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if got.Title != want.Title || !got.Monitored || !got.AddedAt.Equal(now) || got.LastMetadataRefresh != nil ||
		len(got.Tags) != 1 || got.Metadata.AltTitles[0] != "ドクターストーン" || got.Metadata.Links["AniList"] == "" {
		t.Fatalf("series %+v", got)
	}
	var chs []model.Chapter
	_ = d.NewSelect().Model(&chs).Where("series_id = ?", want.ID).Order("number_sort").Scan(ctx)
	if len(chs) != 3 || chs[1].Monitored || !chs[2].Monitored || chs[0].NumberSort != 1.5 {
		t.Fatalf("chapters %+v", chs)
	}
	var p model.Profile
	_ = d.NewSelect().Model(&p).Limit(1).Scan(ctx)
	if !p.IsDefault || len(p.Config.PreferredScanlators) != 1 {
		t.Fatalf("profile %+v", p)
	}
	// new rows get new ids after the copied ones
	extra := &model.Tag{Label: "new in " + string(d.Kind)}
	if _, err := d.NewInsert().Model(extra).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if extra.ID <= 1 {
		t.Fatalf("id sequence not moved: %d", extra.ID)
	}
}

// TestCopy moves a seeded SQLite database to Postgres and back.
func TestCopy(t *testing.T) {
	ctx := context.Background()
	src := dbtest.SQLite(t)
	ser, now := seed(t, src)
	pg := dbtest.Postgres(t)
	var events []string
	res, err := dbcopy.Copy(ctx, src, pg, false, func(table string, done, total int) {
		if done == total && total > 0 {
			events = append(events, table)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Rows["chapters"] != 3 || res.Rows["series"] != 1 || res.Total < 9 || len(events) == 0 {
		t.Fatalf("result %+v %v", res, events)
	}
	check(t, pg, ser, now)

	// a non-empty target needs overwrite
	if _, err := dbcopy.Copy(ctx, src, pg, false, nil); !errors.Is(err, dbcopy.ErrNotEmpty) {
		t.Fatalf("copy into a used database: %v", err)
	}

	// and back into a fresh SQLite file
	back := dbtest.SQLite(t)
	if _, err := dbcopy.Copy(ctx, pg, back, false, nil); err != nil {
		t.Fatal(err)
	}
	check(t, back, ser, now)
	if _, err := dbcopy.Copy(ctx, pg, back, true, nil); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
}

// TestCopySQLite: SQLite to SQLite (backups of a SQLite install use it too).
func TestCopySQLite(t *testing.T) {
	src := dbtest.SQLite(t)
	ser, now := seed(t, src)
	dst := dbtest.SQLite(t)
	if _, err := dbcopy.Copy(context.Background(), src, dst, false, nil); err != nil {
		t.Fatal(err)
	}
	check(t, dst, ser, now)
}
