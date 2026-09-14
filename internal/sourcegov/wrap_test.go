package sourcegov

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/source"
)

// govTestModule implements source.Module plus Maintainer (a capability the
// wrapper doesn't forward) but not Latest.
type govTestModule struct{ maintained bool }

func (m *govTestModule) Test(context.Context) error { return nil }
func (m *govTestModule) Sources(context.Context) ([]source.SourceInfo, error) {
	return []source.SourceInfo{{ID: "A"}}, nil
}
func (m *govTestModule) Search(context.Context, string, string, int) (*source.MangaPage, error) {
	return &source.MangaPage{}, nil
}
func (m *govTestModule) Manga(context.Context, source.MangaRef, bool) (*source.MangaDetails, []source.Chapter, error) {
	return nil, nil, source.ErrNotFound
}
func (m *govTestModule) Pages(_ context.Context, ref source.ChapterRef) ([]source.Page, error) {
	return []source.Page{{Index: 0, URL: "p0"}, {Index: 1, URL: "p1"}}, nil
}
func (m *govTestModule) FetchPage(context.Context, source.Page) (io.ReadCloser, string, error) {
	return io.NopCloser(strings.NewReader("img")), "image/png", nil
}
func (m *govTestModule) Maintain(context.Context) error { m.maintained = true; return nil }

func init() {
	modules.Register(&modules.Implementation{Kind: modules.KindSource, Name: "govtest", DisplayName: "govtest",
		Settings: func() any { return &struct{}{} },
		New:      func(modules.Deps, any) (modules.Instance, error) { return &govTestModule{}, nil }})
}

func TestDecoratorKeepsCapabilities(t *testing.T) {
	d := dbtest.SQLite(t)
	ctx := context.Background()
	g := New(func(Key) model.ThrottleConfig { return Resolve(model.ThrottleConfig{Preset: "fast", MaxConcurrent: 1}) }, nil)
	mods := modules.NewManager(d, nil, slog.New(slog.DiscardHandler), t.TempDir())
	mods.Decorate(modules.KindSource, Decorator(g))
	def := &model.ProviderDefinition{Kind: "source", Implementation: "govtest", Name: "G", Enabled: true}
	if err := mods.Create(ctx, def); err != nil {
		t.Fatal(err)
	}
	m, _, err := modules.GetAs[source.Module](mods, def.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(*Governed); !ok {
		t.Fatalf("source module not wrapped: %T", m)
	}
	// capabilities of the raw module stay reachable
	mt, _, err := modules.GetAs[source.Maintainer](mods, def.ID)
	if err != nil {
		t.Fatalf("maintainer not reachable through the wrapper: %v", err)
	}
	_ = mt.Maintain(ctx)
	// the wrapper implements Latest but reports it unsupported
	l, _, err := modules.GetAs[source.Latest](mods, def.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Latest(ctx, "A", 1); !errors.Is(err, source.ErrUnsupported) {
		t.Fatalf("Latest on a module without it: %v", err)
	}
	// pages carry their catalog and hold a request slot until closed
	pages, err := m.Pages(ctx, source.ChapterRef{Manga: source.MangaRef{SourceID: "A"}})
	if err != nil || pages[0].SourceID != "A" {
		t.Fatalf("pages: %+v %v", pages, err)
	}
	body, _, err := m.FetchPage(ctx, pages[0])
	if err != nil {
		t.Fatal(err)
	}
	cctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, _, err := m.FetchPage(cctx, pages[1]); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second fetch should wait for the first body to close: %v", err)
	}
	_ = body.Close()
	body, _, err = m.FetchPage(ctx, pages[1])
	if err != nil {
		t.Fatal(err)
	}
	_ = body.Close()
}
