package refresh

import (
	"testing"
	"time"
)

func TestChapterTitle(t *testing.T) {
	cases := map[string]string{
		"Vol.16 Ch.139 - #Let Me Tell You The Truth": "#Let Me Tell You The Truth",
		"Chapter 10.5: Omake":                        "Omake",
		"Ch. 3":                                      "",
		"Episode 12 - Fight":                         "Fight",
		"Prologue":                                   "",
		"Volume 2 Chapter 7 | Night":                 "Night",
		"Chapter 78 P":                               "",
		"Chapter 78 - P":                             "P",
		"Chapter 78 The Promise":                     "The Promise",
	}
	for in, want := range cases {
		if got := ChapterTitle(in); got != want {
			t.Errorf("ChapterTitle(%q) = %q want %q", in, got, want)
		}
	}
}

func TestReconciledChapterTitle(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		releases []string
		want     string
		changed  bool
	}{
		{name: "repairs spaced suffix", current: "P", releases: []string{"Episode 78", "Chapter 78 P"}, want: "", changed: true},
		{name: "keeps explicit one-letter title", current: "P", releases: []string{"Episode 78", "Chapter 78 - P"}, want: "P"},
		{name: "uses a current real title", current: "P", releases: []string{"Chapter 78 P", "Chapter 78 - The Promise"}, want: "The Promise", changed: true},
		{name: "does not rewrite normal title", current: "Prologue", releases: []string{"Chapter 78"}, want: "Prologue"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := reconciledChapterTitle(tt.current, tt.releases)
			if got != tt.want || changed != tt.changed {
				t.Fatalf("reconciledChapterTitle(%q, %q) = %q,%v want %q,%v", tt.current, tt.releases, got, changed, tt.want, tt.changed)
			}
		})
	}
}

func TestBackoff(t *testing.T) {
	if Backoff(0) != 0 || Backoff(1) != time.Minute || Backoff(100) != 24*time.Hour {
		t.Fatal("unexpected backoff steps")
	}
}
