package chapternum

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		title, name string
		reported    float64
		want        float64
		ok          bool
	}{
		{"Mokushiroku Alice", "Mokushiroku Alice Vol.1 Ch. 4: Misrepresentation", -1, 4, true},
		{"Bleach", "Bleach 567: Down With Snowwhite", -1, 567, true},
		{"", "Chapter 10.5", -1, 10.5, true},
		{"", "Ch.1044.5 - The Sun", -1, 1044.5, true},
		{"One Piece", "One Piece 12 special", -1, 12.97, true},
		{"", "Vol.2 Chapter 15", -1, 15, true},
		{"", "Episode 3 extra", -1, 3.99, true},
		{"", "Chapter 7b", -1, 7.2, true},
		{"", "Prologue", -1, -1, false},
		{"", "whatever", 12, 12, true},
		{"", "whatever", float64(float32(10.1)), 10.1, true},
		{"Solo Leveling", "Solo Leveling 110", -1, 110, true},
		{"", "Season 2 Episode 5", -1, 5, true},
	}
	for _, c := range cases {
		got, ok := Parse(c.title, c.name, c.reported)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("Parse(%q, %q, %v) = %v,%v want %v,%v", c.title, c.name, c.reported, got, ok, c.want, c.ok)
		}
	}
}

func TestKeyAndFormat(t *testing.T) {
	if k := Key(float64(float32(10.1))); k != "10.1" {
		t.Errorf("Key = %q", k)
	}
	if k := Key(12); k != "12" {
		t.Errorf("Key = %q", k)
	}
	cases := map[float64]string{12: "0012", 1044.5: "1044.5", 3.25: "0003.25", 12345: "12345"}
	for n, want := range cases {
		if got := Format(n, 4); got != want {
			t.Errorf("Format(%v) = %q want %q", n, got, want)
		}
	}
}

func TestVolume(t *testing.T) {
	cases := map[string]string{"Vol.02 Ch.10": "2", "Volume 3 Chapter 1": "3", "Chapter 1": "", "vol 12": "12"}
	for in, want := range cases {
		if got := Volume(in); got != want {
			t.Errorf("Volume(%q) = %q want %q", in, got, want)
		}
	}
}
