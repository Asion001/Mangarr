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
	}
	for in, want := range cases {
		if got := ChapterTitle(in); got != want {
			t.Errorf("ChapterTitle(%q) = %q want %q", in, got, want)
		}
	}
}

func TestBackoff(t *testing.T) {
	if Backoff(0) != 0 || Backoff(1) != time.Minute || Backoff(100) != 24*time.Hour {
		t.Fatal("unexpected backoff steps")
	}
}
