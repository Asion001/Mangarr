package backupimport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"howett.net/plist"
)

// Aidoku backups are Swift Codable structs written as a property list
// (binary, sometimes XML) or, for old versions, JSON with dates in seconds
// since 1970. They're decoded into a generic tree so both shapes (and the
// old string vs new object forms of categories and sources) are accepted.

func parseAidoku(data []byte) (*Backup, error) {
	var root map[string]any
	if _, err := plist.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("aidoku backup: %w", err)
	}
	return aidokuFromTree(root)
}

func parseAidokuJSON(data []byte) (*Backup, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("aidoku backup: %w", err)
	}
	if root["library"] == nil && root["manga"] == nil {
		return nil, ErrUnknownFormat
	}
	return aidokuFromTree(root)
}

type tree map[string]any

func asTree(v any) tree {
	m, _ := v.(map[string]any)
	return m
}

func (t tree) list(key string) []any {
	l, _ := t[key].([]any)
	return l
}

func (t tree) str(key string) string {
	switch v := t[key].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func (t tree) num(key string) (float64, bool) {
	switch v := t[key].(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint64:
		return float64(v), true
	case int:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	}
	return 0, false
}

func (t tree) boolean(key string) bool {
	switch v := t[key].(type) {
	case bool:
		return v
	default:
		n, _ := t.num(key)
		return n != 0
	}
}

func (t tree) time(key string) *time.Time {
	switch v := t[key].(type) {
	case time.Time:
		if v.IsZero() {
			return nil
		}
		u := v.UTC()
		return &u
	default:
		n, ok := t.num(key)
		if !ok || n <= 0 {
			return nil
		}
		sec, frac := math.Modf(n)
		u := time.Unix(int64(sec), int64(frac*1e9)).UTC()
		return &u
	}
}

func (t tree) strings(key string) []string {
	var out []string
	for _, v := range t.list(key) {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func aidokuStatus(n float64) string {
	switch int(n) {
	case 1:
		return "ongoing"
	case 2:
		return "completed"
	case 3:
		return "cancelled"
	case 4:
		return "hiatus"
	}
	return "unknown"
}

var aidokuTrackers = map[string]string{
	"anilist": TrackerAniList, "myanimelist": TrackerMAL, "mangaupdates": TrackerMangaUpdates, "kitsu": TrackerKitsu,
}

// aidokuSourceName guesses a readable name from an id like "en.weebcentral".
func aidokuSourceName(id string) string {
	if i := strings.IndexByte(id, '.'); i >= 0 {
		return id[i+1:]
	}
	return id
}

func aidokuFromTree(root tree) (*Backup, error) {
	b := &Backup{Format: FormatAidoku, Sources: map[string]string{}, Categories: []string{}, Entries: []BackupManga{}}
	if t := root.time("date"); t != nil {
		b.CreatedAt = *t
	}
	for _, v := range root.list("categories") {
		switch c := v.(type) {
		case string:
			b.Categories = append(b.Categories, c)
		case map[string]any:
			if title := tree(c).str("title"); title != "" {
				b.Categories = append(b.Categories, title)
			}
		}
	}
	for _, v := range root.list("sources") {
		switch s := v.(type) {
		case string:
			b.Sources[s] = aidokuSourceName(s)
		case map[string]any:
			if id := tree(s).str("id"); id != "" {
				b.Sources[id] = aidokuSourceName(id)
			}
		}
	}

	type key struct{ source, manga string }
	manga := map[key]tree{}
	for _, v := range root.list("manga") {
		m := asTree(v)
		if m == nil {
			continue
		}
		manga[key{m.str("sourceId"), m.str("id")}] = m
	}
	chapters := map[key][]tree{}
	for _, v := range root.list("chapters") {
		c := asTree(v)
		if c == nil {
			continue
		}
		k := key{c.str("sourceId"), c.str("mangaId")}
		chapters[k] = append(chapters[k], c)
	}
	type hkey struct{ source, manga, chapter string }
	history := map[hkey]tree{}
	for _, v := range root.list("history") {
		h := asTree(v)
		if h == nil {
			continue
		}
		history[hkey{h.str("sourceId"), h.str("mangaId"), h.str("chapterId")}] = h
	}
	trackers := map[key]map[string]string{}
	for _, v := range root.list("trackItems") {
		t := asTree(v)
		if t == nil {
			continue
		}
		name, ok := aidokuTrackers[t.str("trackerId")]
		if !ok || t.str("id") == "" {
			continue
		}
		k := key{t.str("sourceId"), t.str("mangaId")}
		if trackers[k] == nil {
			trackers[k] = map[string]string{}
		}
		trackers[k][name] = t.str("id")
	}

	for _, v := range root.list("library") {
		l := asTree(v)
		if l == nil {
			continue
		}
		k := key{l.str("sourceId"), l.str("mangaId")}
		m := manga[k]
		e := BackupManga{SourceID: k.source, SourceName: b.Sources[k.source], URL: k.manga, Favorite: true, Status: "unknown",
			Categories: l.strings("categories"), Trackers: trackers[k]}
		if e.SourceName == "" {
			e.SourceName = aidokuSourceName(k.source)
		}
		if t := l.time("dateAdded"); t != nil {
			e.AddedAt = *t
		}
		if m != nil {
			e.Title, e.Author, e.Artist, e.Description = m.str("title"), m.str("author"), m.str("artist"), m.str("desc")
			e.Genres, e.ThumbnailURL, e.WebURL = m.strings("tags"), m.str("cover"), m.str("url")
			e.ExcludedScanlators = m.strings("scanlatorFilter")
			if n, ok := m.num("status"); ok {
				e.Status = aidokuStatus(n)
			}
		}
		for _, c := range chapters[k] {
			ch := BackupChapter{URL: c.str("id"), Name: c.str("title"), Scanlator: c.str("scanlator"), Lang: c.str("lang"), Number: -1}
			if n, ok := c.num("chapter"); ok {
				ch.Number = n
			}
			if h := history[hkey{k.source, k.manga, ch.URL}]; h != nil {
				ch.Read = h.boolean("completed")
				if p, ok := h.num("progress"); ok && p > 0 && !ch.Read {
					ch.LastPageRead = int(p) - 1 // Aidoku pages are 1-based
				}
				ch.ReadAt = h.time("dateRead")
			}
			e.Chapters = append(e.Chapters, ch)
		}
		// chapters come in source order; keep a stable number order
		sort.SliceStable(e.Chapters, func(i, j int) bool { return e.Chapters[i].Number > e.Chapters[j].Number })
		if e.Title == "" {
			e.Title = k.manga
		}
		b.Entries = append(b.Entries, e)
	}
	if len(b.Entries) == 0 {
		return nil, errors.New("the backup has no library entries")
	}
	return b, nil
}
