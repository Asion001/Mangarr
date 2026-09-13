package decision

import (
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/model"
)

func ptr[T any](v T) *T { return &v }

func cand(id int64, prio int, scan string, uploaded time.Time) Candidate {
	return Candidate{
		Release: model.ChapterRelease{ID: id, SeriesSourceID: int64(prio), ChapterURL: "/c/" + scan, Scanlator: scan, UploadDate: &uploaded},
		Source:  model.SeriesSource{ID: int64(prio), Priority: prio, Enabled: true},
	}
}

func baseInput() Input {
	return Input{
		Series:  model.Series{Monitored: true},
		Chapter: model.Chapter{ID: 1, Monitored: true, State: model.ChapterMissing},
		Profile: model.Profile{Config: model.ProfileConfig{PreferredScanlators: []string{"^official$", "tcb"}}},
		Now:     time.Now(),
	}
}

func TestRanking(t *testing.T) {
	t0 := time.Now()
	in := baseInput()
	cands := []Candidate{
		cand(1, 1, "random", t0),
		cand(2, 1, "TCB Scans", t0.Add(time.Hour)),
		cand(3, 0, "other", t0.Add(2*time.Hour)),
	}
	d := Decide(in, cands)
	if d.Approved == nil || d.Approved.Release.ID != 3 {
		t.Fatalf("source priority should win, got %+v", d)
	}
	// same source: scanlator preference beats upload date
	d = Decide(in, cands[:2])
	if d.Approved.Release.ID != 2 {
		t.Fatalf("preferred scanlator should win, got %d", d.Approved.Release.ID)
	}
	// equal rank: earliest upload wins
	d = Decide(in, []Candidate{cand(5, 1, "x", t0.Add(time.Hour)), cand(6, 1, "y", t0)})
	if d.Approved.Release.ID != 6 {
		t.Fatalf("earliest upload should win, got %d", d.Approved.Release.ID)
	}
}

func TestRejections(t *testing.T) {
	t0 := time.Now()
	in := baseInput()
	in.Profile.Config.BlockedScanlators = []string{"bad"}
	backoff := cand(2, 0, "ok", t0)
	backoff.Source.BackoffUntil = ptr(t0.Add(time.Hour))
	blocklisted := cand(3, 1, "fine", t0)
	in.Blocklisted = func(ss int64, url string) bool { return url == "/c/fine" }
	d := Decide(in, []Candidate{cand(1, 0, "Bad Group", t0), backoff, blocklisted})
	if d.Approved != nil || len(d.Rejections) != 3 {
		t.Fatalf("expected all rejected: %+v", d)
	}
	if !d.Rejections[1].Temporary {
		t.Fatal("backoff rejection should be temporary")
	}

	in = baseInput()
	in.Chapter.Monitored = false
	if d := Decide(in, []Candidate{cand(1, 0, "a", t0)}); d.Approved != nil {
		t.Fatal("unmonitored chapter must not be grabbed")
	}
	in.Search = true
	if d := Decide(in, []Candidate{cand(1, 0, "a", t0)}); d.Approved == nil {
		t.Fatal("explicit search ignores monitoring")
	}

	in = baseInput()
	in.Chapter.State = model.ChapterCleaned
	if d := Decide(in, []Candidate{cand(1, 0, "a", t0)}); d.Approved != nil {
		t.Fatal("cleaned chapters must not be re-downloaded")
	}
}

func TestUpgrades(t *testing.T) {
	t0 := time.Now()
	in := baseInput()
	cur := cand(1, 1, "random", t0)
	in.CurrentFile = &model.ChapterFile{ReleaseID: ptr(int64(1))}
	in.CurrentRelease = &cur
	if d := Decide(in, []Candidate{cur, cand(2, 1, "official", t0)}); d.Approved != nil {
		t.Fatal("upgrades disabled: nothing should be approved")
	}
	in.Profile.Config.AllowUpgrades = true
	d := Decide(in, []Candidate{cur, cand(2, 1, "official", t0)})
	if d.Approved == nil || d.Approved.Release.ID != 2 || !d.IsUpgrade {
		t.Fatalf("expected upgrade to official, got %+v", d)
	}
	if d := Decide(in, []Candidate{cur, cand(3, 1, "another random", t0.Add(-time.Hour))}); d.Approved != nil {
		t.Fatal("equal rank (even if earlier) must not trigger an upgrade")
	}
}
