package api

import (
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/model"
)

func TestCatalogHealth(t *testing.T) {
	old, recent := time.Now().Add(-48*time.Hour), time.Now()
	links := []model.SeriesSource{
		{SeriesID: 1, ModuleID: 1, SourceID: "wc", Enabled: true, LastSuccessAt: &old},
		{SeriesID: 2, ModuleID: 1, SourceID: "wc", Enabled: true, ConsecutiveFailures: 3, LastSuccessAt: &recent},
		{SeriesID: 3, ModuleID: 1, SourceID: "wc", Enabled: false, ConsecutiveFailures: 9}, // off: not "failing"
		{SeriesID: 1, ModuleID: 2, SourceID: "md", Enabled: true},
	}
	got := catalogHealth(links)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	wc := got[0]
	if wc.SourceID != "wc" || wc.Series != 3 || wc.Failing != 1 || wc.LastSuccessAt == nil || !wc.LastSuccessAt.Equal(recent) {
		t.Fatalf("wc: %+v", wc)
	}
	if md := got[1]; md.Series != 1 || md.Failing != 0 || md.LastSuccessAt != nil {
		t.Fatalf("md: %+v", md)
	}
}
