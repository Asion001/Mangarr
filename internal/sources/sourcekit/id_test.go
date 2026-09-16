package sourcekit

import "testing"

// TestKeiyoushiID pins the id derivation to ids that are already out in the
// world: get this wrong and a library imported from a Mihon backup links to a
// catalog nobody has.
func TestKeiyoushiID(t *testing.T) {
	for _, c := range []struct {
		name, lang string
		version    int
		want       string
	}{
		{"MangaDex", "en", 1, "2499283573021220255"},
		{"Weeb Central", "en", 1, "2131019126180322627"},
		{"Atsumaru", "en", 2, "2327480808438768017"},
		{"MangaLib", "ru", 1, "6111047689498497237"},
		{"Senkuro", "ru", 1, "1452500215874655811"},
	} {
		if got := KeiyoushiID(c.name, c.lang, c.version); got != c.want {
			t.Errorf("KeiyoushiID(%q, %q, %d) = %s, want %s", c.name, c.lang, c.version, got, c.want)
		}
	}
	// the name is matched case-insensitively, the language is not
	if KeiyoushiID("mangadex", "en", 1) != KeiyoushiID("MangaDex", "en", 1) {
		t.Error("the name should be case-insensitive")
	}
	if KeiyoushiID("MangaDex", "EN", 1) == KeiyoushiID("MangaDex", "en", 1) {
		t.Error("the language is part of the id as written")
	}
}
