package backupimport

import (
	"os"
	"testing"
)

func TestRealBackup(t *testing.T) {
	path := os.Getenv("MANGARR_TEST_BACKUP")
	if path == "" {
		t.Skip("MANGARR_TEST_BACKUP not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %d entries, categories %v, sources %v", b.Format, len(b.Entries), b.Categories, b.Sources)
	for _, e := range b.Entries {
		from, ok := e.ResumeFrom()
		t.Logf("%s | %s %s | fav=%v status=%s cats=%v trackers=%v chapters=%d read=%d from=%v/%v", e.Title, e.SourceID, e.URL, e.Favorite, e.Status, e.Categories, e.Trackers, len(e.Chapters), e.ReadCount(), from, ok)
	}
}
