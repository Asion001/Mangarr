package metadataagg

import (
	"testing"

	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules/metadata"
	"github.com/Asion001/mangarr/internal/modules/source"
)

func TestMergePriorityAndUnion(t *testing.T) {
	a := metadata.SeriesMetadata{Provider: "anilist", ID: "1", Title: "Title A", Genres: []string{"Action"}, Status: "ongoing",
		ExternalIDs: map[string]string{"mal": "9"}}
	b := metadata.SeriesMetadata{Provider: "mangaupdates", ID: "x", Title: "Title B", Description: "desc B", Genres: []string{"action", "Drama"}, Publisher: "Pub"}
	src := FromSource(&source.MangaDetails{Manga: source.Manga{Title: "T"}, Author: "A, B", Genres: []string{"Content rating: Safe", "Comedy"}, Status: source.StatusUnknown})
	out, prov := Merge([]metadata.SeriesMetadata{a, b, src})
	if out.Title != "Title A" || out.Description != "desc B" || out.Publisher != "Pub" || out.Status != "ongoing" {
		t.Fatalf("scalars: %+v", out)
	}
	if len(out.Genres) != 3 || out.Genres[2] != "Comedy" {
		t.Fatalf("genres: %v", out.Genres)
	}
	if len(out.Authors) != 2 || prov["authors"] != "source" || prov["description"] != "mangaupdates" || prov["title"] != "anilist" {
		t.Fatalf("authors/provenance: %v %v", out.Authors, prov)
	}
	if out.ExternalIDs["anilist"] != "1" || out.ExternalIDs["mangaupdates"] != "x" || out.ExternalIDs["mal"] != "9" {
		t.Fatalf("ids: %v", out.ExternalIDs)
	}
	found := false
	for _, alt := range out.AltTitles {
		if alt == "Title B" {
			found = true
		}
	}
	if !found {
		t.Fatalf("alt titles: %v", out.AltTitles)
	}
}

func TestApplyRespectsLocks(t *testing.T) {
	s := &model.Series{Title: "My Title", Status: "ongoing", ReadingDirection: "rtl",
		Metadata: model.SeriesMetadata{Description: "mine", Locks: []string{"title", "description"}}}
	r := &Resolved{Metadata: metadata.SeriesMetadata{Title: "Other", Description: "theirs", Status: "completed", Format: "manhwa", Genres: []string{"A"}},
		Provenance: map[string]string{"title": "anilist", "description": "anilist", "genres": "anilist"}}
	if !Apply(s, r) {
		t.Fatal("expected change")
	}
	if s.Title != "My Title" || s.Metadata.Description != "mine" {
		t.Fatalf("locked fields changed: %+v", s)
	}
	if s.Status != "completed" || s.ReadingDirection != "webtoon" || len(s.Metadata.Genres) != 1 {
		t.Fatalf("unlocked fields not applied: %+v", s)
	}
	if s.Metadata.Provenance["title"] != "" || s.Metadata.Provenance["genres"] != "anilist" {
		t.Fatalf("provenance: %v", s.Metadata.Provenance)
	}
}

func TestFindSame(t *testing.T) {
	list := []Candidate{{SeriesMetadata: metadata.SeriesMetadata{Provider: "anilist", ExternalIDs: map[string]string{"mal": "13"}}}}
	c := Candidate{SeriesMetadata: metadata.SeriesMetadata{Provider: "mangaupdates", ExternalIDs: map[string]string{"mal": "13"}}}
	if findSame(list, c) != 0 {
		t.Fatal("expected match by MAL id")
	}
	if Normalize("Re:Zero - Starting Life") != "rezerostartinglife" {
		t.Fatal(Normalize("Re:Zero - Starting Life"))
	}
}
