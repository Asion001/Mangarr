package komgaapi

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Asion001/mangarr/internal/reading"
	"github.com/Asion001/mangarr/internal/titlematch"
)

// condition is a Komga search condition: {"allOf": [...]}, {"anyOf": [...]}
// or {"field": {"operator": "is", "value": ...}}. Several keys in one
// object are ANDed (Paperback 0.9 sends sibling keys without allOf).
type condition struct {
	AllOf  []condition
	AnyOf  []condition
	Not    *condition
	Fields []fieldCond
}

type fieldCond struct {
	Field    string
	Operator string // lower case: is, isnot, istrue, isfalse, after, before, contains, …
	Value    json.RawMessage
}

func (c *condition) UnmarshalJSON(b []byte) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	for k, raw := range obj {
		switch k {
		case "allOf":
			if err := json.Unmarshal(raw, &c.AllOf); err != nil {
				return err
			}
		case "anyOf":
			if err := json.Unmarshal(raw, &c.AnyOf); err != nil {
				return err
			}
		case "not":
			var n condition
			if err := json.Unmarshal(raw, &n); err != nil {
				return err
			}
			c.Not = &n
		default:
			var fc struct {
				Operator string          `json:"operator"`
				Value    json.RawMessage `json:"value"`
			}
			if err := json.Unmarshal(raw, &fc); err != nil {
				continue // not a field condition: ignore
			}
			c.Fields = append(c.Fields, fieldCond{Field: k, Operator: strings.ToLower(fc.Operator), Value: fc.Value})
		}
	}
	return nil
}

// eval evaluates c; match answers one field condition (known=false
// matches, so unsupported fields never hide everything).
func (c *condition) eval(match func(fieldCond) (ok, known bool)) bool {
	if c == nil {
		return true
	}
	for i := range c.AllOf {
		if !c.AllOf[i].eval(match) {
			return false
		}
	}
	if len(c.AnyOf) > 0 {
		any := false
		for i := range c.AnyOf {
			if c.AnyOf[i].eval(match) {
				any = true
				break
			}
		}
		if !any {
			return false
		}
	}
	if c.Not != nil && c.Not.eval(match) {
		return false
	}
	for _, f := range c.Fields {
		if ok, known := match(f); known && !ok {
			return false
		}
	}
	return true
}

// str returns a condition's value as a string (values may be strings,
// numbers or {"name","role"} objects).
func (f fieldCond) str() string {
	var s string
	if json.Unmarshal(f.Value, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(f.Value, &n) == nil {
		return n.String()
	}
	var obj map[string]any
	if json.Unmarshal(f.Value, &obj) == nil {
		if name, ok := obj["name"].(string); ok {
			return name
		}
	}
	return strings.Trim(string(f.Value), `"`)
}

// matchString applies an operator to a value set (a field may have several
// values, e.g. tags).
func (f fieldCond) matchString(values ...string) bool {
	want := f.str()
	has := func(fn func(string) bool) bool { return slices.ContainsFunc(values, fn) }
	switch f.Operator {
	case "is":
		return has(func(v string) bool { return strings.EqualFold(v, want) })
	case "isnot":
		return !has(func(v string) bool { return strings.EqualFold(v, want) })
	case "contains":
		return has(func(v string) bool { return strings.Contains(strings.ToLower(v), strings.ToLower(want)) })
	case "doesnotcontain":
		return !has(func(v string) bool { return strings.Contains(strings.ToLower(v), strings.ToLower(want)) })
	case "beginswith":
		return has(func(v string) bool { return strings.HasPrefix(strings.ToLower(v), strings.ToLower(want)) })
	case "doesnotbeginwith":
		return !has(func(v string) bool { return strings.HasPrefix(strings.ToLower(v), strings.ToLower(want)) })
	case "endswith":
		return has(func(v string) bool { return strings.HasSuffix(strings.ToLower(v), strings.ToLower(want)) })
	case "doesnotendwith":
		return !has(func(v string) bool { return strings.HasSuffix(strings.ToLower(v), strings.ToLower(want)) })
	case "isnull":
		return len(values) == 0 || (len(values) == 1 && values[0] == "")
	case "isnotnull":
		return !(len(values) == 0 || (len(values) == 1 && values[0] == ""))
	}
	return true
}

func (f fieldCond) matchBool(v bool) bool {
	switch f.Operator {
	case "istrue":
		return v
	case "isfalse":
		return !v
	}
	return true
}

func (f fieldCond) matchTime(t *time.Time) bool {
	want, err := time.Parse(time.RFC3339, f.str())
	if err != nil {
		if want, err = time.Parse("2006-01-02", f.str()); err != nil {
			return true
		}
	}
	switch f.Operator {
	case "after":
		return t != nil && t.After(want)
	case "before":
		return t != nil && t.Before(want)
	case "isnull":
		return t == nil
	case "isnotnull":
		return t != nil
	}
	return true
}

// searchMatches matches a title and its alternatives against a search.
func searchMatches(search string, titles ...string) bool {
	q := titlematch.Normalize(search)
	if q == "" {
		return true
	}
	words := strings.Fields(q)
	for _, t := range titles {
		n := titlematch.Normalize(t)
		all := true
		for _, w := range words {
			if !strings.Contains(n, w) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// seriesFilter is a series query (GET parameters and/or a condition).
type seriesFilter struct {
	Search      string
	LibraryIDs  []string
	Statuses    []string
	ReadStatus  []string
	Genres      []string
	Tags        []string
	Publishers  []string
	Collections []string
	Languages   []string
	Authors     []string
	Cond        *condition
}

// listParam reads a parameter that may be repeated and/or comma-separated.
func listParam(r *http.Request, names ...string) []string {
	var out []string
	for _, n := range names {
		for _, v := range r.URL.Query()[n] {
			for _, p := range strings.Split(v, ",") {
				if p = strings.TrimSpace(p); p != "" {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

func seriesFilterFrom(r *http.Request) seriesFilter {
	f := seriesFilter{Search: r.URL.Query().Get("search"), LibraryIDs: listParam(r, "library_id"), Statuses: listParam(r, "status"),
		ReadStatus: listParam(r, "read_status"), Genres: listParam(r, "genre"), Tags: listParam(r, "tag"), Publishers: listParam(r, "publisher"),
		Collections: listParam(r, "collection_id"), Languages: listParam(r, "language")}
	for _, a := range r.URL.Query()["author"] {
		name, _, _ := strings.Cut(a, ",")
		f.Authors = append(f.Authors, name)
	}
	return f
}

func anyFold(set []string, values ...string) bool {
	if len(set) == 0 {
		return true
	}
	for _, s := range set {
		for _, v := range values {
			if strings.EqualFold(s, v) {
				return true
			}
		}
	}
	return false
}

func (f seriesFilter) collections(si reading.SeriesInfo) []string {
	out := make([]string, 0, len(si.Series.Tags))
	for _, t := range si.Series.Tags {
		out = append(out, id(t))
	}
	return out
}

func (f seriesFilter) match(si reading.SeriesInfo) bool {
	ser := si.Series
	md := ser.Metadata
	if f.Search != "" && !searchMatches(f.Search, append([]string{ser.Title}, md.AltTitles...)...) {
		return false
	}
	people := append(append([]string{}, md.Authors...), md.Artists...)
	if !anyFold(f.LibraryIDs, id(ser.RootFolderID)) || !anyFold(f.Statuses, seriesStatus(ser.Status)) ||
		!anyFold(f.ReadStatus, seriesReadStatus(si)) || !anyFold(f.Genres, md.Genres...) || !anyFold(f.Tags, md.Tags...) ||
		!anyFold(f.Publishers, md.Publisher) || !anyFold(f.Collections, f.collections(si)...) || !anyFold(f.Languages, ser.Language) ||
		!anyFold(f.Authors, people...) {
		return false
	}
	return f.Cond.eval(func(c fieldCond) (bool, bool) {
		switch c.Field {
		case "libraryId":
			return c.matchString(id(ser.RootFolderID)), true
		case "seriesId":
			return c.matchString(id(ser.ID)), true
		case "collectionId":
			return c.matchString(f.collections(si)...), true
		case "readStatus":
			return c.matchString(seriesReadStatus(si)), true
		case "seriesStatus", "status":
			return c.matchString(seriesStatus(ser.Status)), true
		case "deleted", "oneshot":
			return c.matchBool(false), true
		case "complete":
			return c.matchBool(ser.Status == "completed" && si.Read >= si.Books), true
		case "tag":
			return c.matchString(md.Tags...), true
		case "genre":
			return c.matchString(md.Genres...), true
		case "publisher":
			return c.matchString(md.Publisher), true
		case "language":
			return c.matchString(ser.Language), true
		case "author":
			return c.matchString(people...), true
		case "title":
			return c.matchString(append([]string{ser.Title}, md.AltTitles...)...), true
		case "releaseDate":
			return c.matchTime(si.FirstRelease), true
		}
		return true, false
	})
}

// sortSeries applies Komga sort parameters ("metadata.titleSort,asc").
func sortSeries(list []reading.SeriesInfo, sorts []string) {
	for i := len(sorts) - 1; i >= 0; i-- { // stable: the first sort wins
		field, dir, _ := strings.Cut(sorts[i], ",")
		desc := strings.EqualFold(dir, "desc")
		var less func(a, b reading.SeriesInfo) bool
		switch field {
		case "metadata.titleSort", "titleSort", "name", "metadata.title", "relevance":
			less = func(a, b reading.SeriesInfo) bool { return a.Series.SortTitle < b.Series.SortTitle }
		case "lastModified", "lastModifiedDate":
			less = func(a, b reading.SeriesInfo) bool { return a.LastModified().Before(b.LastModified()) }
		case "created", "createdDate":
			less = func(a, b reading.SeriesInfo) bool { return a.Series.AddedAt.Before(b.Series.AddedAt) }
		case "booksCount":
			less = func(a, b reading.SeriesInfo) bool { return a.Books < b.Books }
		case "booksMetadata.releaseDate":
			less = func(a, b reading.SeriesInfo) bool { return timeOr(a.LastRelease).Before(timeOr(b.LastRelease)) }
		case "readProgress.readDate", "readDate":
			less = func(a, b reading.SeriesInfo) bool { return timeOr(a.LastRead).Before(timeOr(b.LastRead)) }
		case "random":
			rand.Shuffle(len(list), func(i, j int) { list[i], list[j] = list[j], list[i] })
			continue
		default:
			continue
		}
		sort.SliceStable(list, func(i, j int) bool {
			if desc {
				return less(list[j], list[i])
			}
			return less(list[i], list[j])
		})
	}
}

func timeOr(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// bookFilter is a book query.
type bookFilter struct {
	Search      string
	SeriesIDs   []string
	LibraryIDs  []string
	ReadStatus  []string
	MediaStatus []string
	ReadListIDs []string
	Cond        *condition
	seriesLib   map[int64]int64 // series id -> library id
	readList    map[int64]bool  // chapters in the "continue reading" list
}

func bookFilterFrom(r *http.Request) bookFilter {
	return bookFilter{Search: r.URL.Query().Get("search"), SeriesIDs: listParam(r, "series_id"), LibraryIDs: listParam(r, "library_id"),
		ReadStatus: listParam(r, "read_status"), MediaStatus: listParam(r, "media_status")}
}

func (f bookFilter) match(b reading.BookInfo) bool {
	ch := b.Chapter
	lib := id(f.seriesLib[ch.SeriesID])
	if f.Search != "" && !searchMatches(f.Search, chapterTitle(ch)) {
		return false
	}
	if !anyFold(f.SeriesIDs, id(ch.SeriesID)) || !anyFold(f.LibraryIDs, lib) || !anyFold(f.ReadStatus, bookReadStatus(b)) ||
		!anyFold(f.MediaStatus, "READY") {
		return false
	}
	if len(f.ReadListIDs) > 0 && !f.readList[ch.ID] {
		return false
	}
	return f.Cond.eval(func(c fieldCond) (bool, bool) {
		switch c.Field {
		case "seriesId":
			return c.matchString(id(ch.SeriesID)), true
		case "libraryId":
			return c.matchString(lib), true
		case "readStatus":
			return c.matchString(bookReadStatus(b)), true
		case "mediaStatus":
			return c.matchString("READY"), true
		case "mediaProfile":
			return c.matchString("DIVINA"), true
		case "deleted", "oneshot":
			return c.matchBool(false), true
		case "readListId":
			return f.readList[ch.ID] == (c.Operator == "is"), true
		case "releaseDate":
			return c.matchTime(ch.ReleaseDate), true
		case "title":
			return c.matchString(chapterTitle(ch)), true
		case "numberSort":
			return true, false
		}
		return true, false
	})
}

// sortBooks applies Komga book sorts ("series,metadata.numberSort" etc.).
func sortBooks(list []reading.BookInfo, sorts []string, seriesTitle map[int64]string) {
	for i := len(sorts) - 1; i >= 0; i-- {
		field, dir, _ := strings.Cut(sorts[i], ",")
		desc := strings.EqualFold(dir, "desc")
		if strings.EqualFold(dir, "metadata.numberSort") { // "series,metadata.numberSort"
			field, desc = "series", false
		}
		var less func(a, b reading.BookInfo) bool
		switch field {
		case "metadata.numberSort", "numberSort", "number", "metadata.number":
			less = func(a, b reading.BookInfo) bool { return a.Chapter.NumberSort < b.Chapter.NumberSort }
		case "series", "seriesTitle":
			less = func(a, b reading.BookInfo) bool {
				ta, tb := seriesTitle[a.Chapter.SeriesID], seriesTitle[b.Chapter.SeriesID]
				if ta != tb {
					return ta < tb
				}
				return a.Chapter.NumberSort < b.Chapter.NumberSort
			}
		case "created", "createdDate":
			less = func(a, b reading.BookInfo) bool { return a.Chapter.FirstSeenAt.Before(b.Chapter.FirstSeenAt) }
		case "lastModified", "lastModifiedDate":
			less = func(a, b reading.BookInfo) bool { return a.Chapter.UpdatedAt.Before(b.Chapter.UpdatedAt) }
		case "metadata.releaseDate", "releaseDate":
			less = func(a, b reading.BookInfo) bool {
				return timeOr(a.Chapter.ReleaseDate).Before(timeOr(b.Chapter.ReleaseDate))
			}
		case "readProgress.readDate", "readDate":
			less = func(a, b reading.BookInfo) bool { return readAt(a).Before(readAt(b)) }
		case "media.pagesCount":
			less = func(a, b reading.BookInfo) bool { return pagesOf(a) < pagesOf(b) }
		case "name", "metadata.title":
			less = func(a, b reading.BookInfo) bool { return chapterTitle(a.Chapter) < chapterTitle(b.Chapter) }
		default:
			continue
		}
		sort.SliceStable(list, func(i, j int) bool {
			if desc {
				return less(list[j], list[i])
			}
			return less(list[i], list[j])
		})
	}
}

func readAt(b reading.BookInfo) time.Time {
	if b.State == nil {
		return time.Time{}
	}
	if b.State.ReadAt != nil {
		return *b.State.ReadAt
	}
	return b.State.SyncedAt
}

func pagesOf(b reading.BookInfo) int {
	if b.File != nil {
		return b.File.PageCount
	}
	return 0 // streamed page counts don't sort
}
