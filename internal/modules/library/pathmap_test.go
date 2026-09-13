package library

import "testing"

func TestPathMap(t *testing.T) {
	pm := NewPathMap(map[string]string{"/data/manga": "/books/manga", "/data/manga/en": "/en"})
	cases := []struct{ local, remote string }{
		{"/data/manga/en/One Piece", "/en/One Piece"},
		{"/data/manga/ja/X", "/books/manga/ja/X"},
		{"/other/path", "/other/path"},
		{"/data/mangaextra", "/data/mangaextra"},
	}
	for _, c := range cases {
		if got := pm.ToRemote(c.local); got != c.remote {
			t.Errorf("ToRemote(%q) = %q want %q", c.local, got, c.remote)
		}
	}
	if got := pm.ToLocal("/en/One Piece/x.cbz"); got != "/data/manga/en/One Piece/x.cbz" {
		t.Errorf("ToLocal = %q", got)
	}
	if !Under("/a/b/c", "/a/b") || Under("/a/bc", "/a/b") || !Under("/a/b", "/a/b/") {
		t.Error("Under")
	}
}
