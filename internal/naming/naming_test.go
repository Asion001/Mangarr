package naming

import "testing"

func TestRender(t *testing.T) {
	v := Values{SeriesTitle: "Re:Zero", Chapter: 12, HasChapter: true, Scanlator: "Group/A"}
	cases := map[string]string{
		"{Series Title} Ch.{Chapter:0000}":                  "Re-Zero Ch.0012",
		"{Series Title}[ Vol.{Volume:00}] Ch.{Chapter:000}": "Re-Zero Ch.012",
		"{Series CleanTitle} - {Chapter} [{Scanlator}]":     "ReZero - 12 Group-A",
		"{Series Title}[ ({Series Year})]":                  "Re-Zero",
	}
	for tpl, want := range cases {
		if got := Render(tpl, v); got != want {
			t.Errorf("Render(%q) = %q want %q", tpl, got, want)
		}
	}
	v.Volume = "3"
	v.Chapter = 10.5
	v.SeriesYear = 2014
	if got := Render("{Series Title}[ Vol.{Volume:00}] Ch.{Chapter:0000}", v); got != "Re-Zero Vol.03 Ch.0010.5" {
		t.Errorf("got %q", got)
	}
	if got := Render("{Series Title}[ ({Series Year})]", v); got != "Re-Zero (2014)" {
		t.Errorf("got %q", got)
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"  a/b\\c:d*e?  ":  "a-b-c-de",
		"trailing dots...": "trailing dots",
		"":                 "_",
		"x\x00y":           "xy",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q want %q", in, got, want)
		}
	}
}

func TestSortTitle(t *testing.T) {
	if SortTitle("The Beginning After The End") != "beginning after the end" {
		t.Error(SortTitle("The Beginning After The End"))
	}
}
