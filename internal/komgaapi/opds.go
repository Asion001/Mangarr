package komgaapi

import (
	"archive/zip"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Asion001/mangarr/internal/access"
	"github.com/Asion001/mangarr/internal/cbz"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/reading"
)

type atomFeed struct {
	XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Title   string      `xml:"title"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Links   []atomLink  `xml:"link"`
	Entries []atomEntry `xml:"entry"`
}

type atomLink struct {
	Rel   string `xml:"rel,attr,omitempty"`
	Href  string `xml:"href,attr"`
	Type  string `xml:"type,attr,omitempty"`
	Title string `xml:"title,attr,omitempty"`
}

type atomEntry struct {
	Title   string     `xml:"title"`
	ID      string     `xml:"id"`
	Updated string     `xml:"updated"`
	Summary string     `xml:"summary,omitempty"`
	Authors []atomText `xml:"author"`
	Links   []atomLink `xml:"link"`
}

type atomText struct {
	Name string `xml:"name"`
}

type opdsHandlers struct{ s *Service }

func (h *opdsHandlers) identity(w http.ResponseWriter, r *http.Request) (int64, bool) {
	return h.s.readerID(w, r)
}

func (h *opdsHandlers) feed(w http.ResponseWriter, title, id string, entries []atomEntry, links ...atomLink) {
	f := atomFeed{Title: title, ID: id, Updated: time.Now().UTC().Format(time.RFC3339), Entries: entries, Links: links}
	w.Header().Set("Content-Type", "application/atom+xml;profile=opds-catalog;kind=navigation;charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(f)
}

func link(rel, href, typ, title string) atomLink {
	return atomLink{Rel: rel, Href: href, Type: typ, Title: title}
}

func (h *opdsHandlers) root(w http.ResponseWriter, r *http.Request) {
	entries := []atomEntry{
		{Title: "Libraries", ID: "urn:mangarr:opds:libraries", Updated: time.Now().UTC().Format(time.RFC3339), Links: []atomLink{link("subsection", "/opds/libraries", "application/atom+xml;profile=opds-catalog;kind=navigation", "Libraries")}},
		{Title: "Recently updated", ID: "urn:mangarr:opds:updated", Updated: time.Now().UTC().Format(time.RFC3339), Links: []atomLink{link("subsection", "/opds/updated", "application/atom+xml;profile=opds-catalog;kind=acquisition", "Recently updated")}},
	}
	links := []atomLink{link("self", "/opds", "application/atom+xml;profile=opds-catalog;kind=navigation", "Catalog"), link("search", "/opds/search.xml", "application/opensearchdescription+xml", "Search")}
	h.feed(w, "mangarr", "/opds", entries, links...)
}

func (h *opdsHandlers) searchDescription(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/opensearchdescription+xml;charset=utf-8")
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/"><ShortName>mangarr</ShortName><Description>Search the mangarr catalog</Description><Url type="application/atom+xml;profile=opds-catalog;kind=acquisition" template="/opds/search?q={searchTerms}"/></OpenSearchDescription>`))
}

func pageBounds(r *http.Request, n int) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("_count"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	start, _ := strconv.Atoi(r.URL.Query().Get("_start"))
	if start < 0 {
		start = 0
	}
	if p, _ := strconv.Atoi(r.URL.Query().Get("_page")); p > 1 && r.URL.Query().Get("_start") == "" {
		start = (p - 1) * limit
	}
	if start > n {
		start = n
	}
	end := start + limit
	if end > n {
		end = n
	}
	return start, end
}

func (h *opdsHandlers) libraries(w http.ResponseWriter, r *http.Request) {
	rid, ok := h.identity(w, r)
	if !ok {
		return
	}
	all, err := h.s.deps.Reading.AllSeries(r.Context(), rid, 0)
	if err != nil {
		writeError(w, r, 500, err.Error())
		return
	}
	seen := map[int64]bool{}
	entries := []atomEntry{}
	for _, si := range all {
		if seen[si.Series.RootFolderID] {
			continue
		}
		seen[si.Series.RootFolderID] = true
		var folder model.RootFolder
		if err := h.s.deps.DB.NewSelect().Model(&folder).Where("id = ?", si.Series.RootFolderID).Scan(r.Context()); err != nil {
			continue
		}
		entries = append(entries, atomEntry{Title: path.Base(folder.Path), ID: fmt.Sprintf("urn:mangarr:library:%d", folder.ID), Updated: time.Now().UTC().Format(time.RFC3339), Links: []atomLink{link("subsection", fmt.Sprintf("/opds/libraries/%d", folder.ID), "application/atom+xml;profile=opds-catalog;kind=acquisition", folder.Path)}})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Title < entries[j].Title })
	start, end := pageBounds(r, len(entries))
	h.paged(w, r, "Libraries", "/opds/libraries", entries, start, end)
}

func (h *opdsHandlers) paged(w http.ResponseWriter, r *http.Request, title, id string, entries []atomEntry, start, end int) {
	links := []atomLink{link("self", id, "application/atom+xml;profile=opds-catalog;kind=navigation", title)}
	pageURL := func(n int) string {
		u, err := url.Parse(id)
		if err != nil {
			return id
		}
		q := u.Query()
		q.Set("_start", strconv.Itoa(n))
		u.RawQuery = q.Encode()
		return u.String()
	}
	if start > 0 {
		links = append(links, link("previous", pageURL(max(0, start-20)), "application/atom+xml;profile=opds-catalog;kind=acquisition", "Previous"))
	}
	if end < len(entries) {
		links = append(links, link("next", pageURL(end), "application/atom+xml;profile=opds-catalog;kind=acquisition", "Next"))
	}
	h.feed(w, title, id, entries[start:end], links...)
}

func (h *opdsHandlers) library(w http.ResponseWriter, r *http.Request) {
	rid, ok := h.identity(w, r)
	if !ok {
		return
	}
	folderID := pathID(r, "id")
	all, err := h.s.deps.Reading.AllSeries(r.Context(), rid, 0)
	if err != nil {
		writeError(w, r, 500, err.Error())
		return
	}
	entries := []atomEntry{}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	for _, si := range all {
		if si.Series.RootFolderID != folderID || (q != "" && !strings.Contains(strings.ToLower(si.Series.Title), q) && !strings.Contains(strings.ToLower(si.Series.SortTitle), q)) {
			continue
		}
		entries = append(entries, seriesEntry(si))
	}
	start, end := pageBounds(r, len(entries))
	h.paged(w, r, "Series", r.URL.Path, entries, start, end)
}

func seriesEntry(si reading.SeriesInfo) atomEntry {
	return atomEntry{Title: si.Series.Title, ID: fmt.Sprintf("urn:mangarr:series:%d", si.Series.ID), Updated: si.LastModified().UTC().Format(time.RFC3339), Summary: si.Series.Metadata.Description, Links: []atomLink{link("subsection", fmt.Sprintf("/opds/series/%d", si.Series.ID), "application/atom+xml;profile=opds-catalog;kind=acquisition", "Chapters"), link("http://opds-spec.org/image", fmt.Sprintf("/opds/covers/series/%d", si.Series.ID), "image/jpeg", "Cover"), link("http://opds-spec.org/image/thumbnail", fmt.Sprintf("/opds/covers/series/%d", si.Series.ID), "image/jpeg", "Cover thumbnail")}}
}

func (h *opdsHandlers) series(w http.ResponseWriter, r *http.Request) {
	rid, ok := h.identity(w, r)
	if !ok {
		return
	}
	sid := pathID(r, "id")
	si, err := h.s.deps.Reading.Series(r.Context(), rid, sid)
	if err != nil {
		notFoundOr500(w, r, err)
		return
	}
	books, err := h.s.deps.Reading.Books(r.Context(), rid, sid)
	if err != nil {
		writeError(w, r, 500, err.Error())
		return
	}
	entries := make([]atomEntry, 0, len(books))
	for _, b := range books {
		title := b.Chapter.Title
		if strings.TrimSpace(title) == "" {
			title = fmt.Sprintf("Chapter %g", b.Chapter.NumberSort)
		}
		cover := fmt.Sprintf("/opds/covers/chapters/%d", b.Chapter.ID)
		entries = append(entries, atomEntry{Title: title, ID: fmt.Sprintf("urn:mangarr:chapter:%d", b.Chapter.ID), Updated: b.Chapter.UpdatedAt.UTC().Format(time.RFC3339), Links: []atomLink{link("http://opds-spec.org/acquisition/open-access", fmt.Sprintf("/opds/chapters/%d", b.Chapter.ID), "application/vnd.comicbook+zip", "Download CBZ"), link("http://opds-spec.org/image", cover, "image/jpeg", "Cover"), link("http://opds-spec.org/image/thumbnail", cover, "image/jpeg", "Cover thumbnail")}})
	}
	start, end := pageBounds(r, len(entries))
	h.paged(w, r, si.Series.Title, fmt.Sprintf("/opds/series/%d", sid), entries, start, end)
}

func (h *opdsHandlers) search(w http.ResponseWriter, r *http.Request) {
	rid, ok := h.identity(w, r)
	if !ok {
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	all, err := h.s.deps.Reading.AllSeries(r.Context(), rid, 0)
	if err != nil {
		writeError(w, r, 500, err.Error())
		return
	}
	entries := []atomEntry{}
	for _, si := range all {
		if q == "" || strings.Contains(strings.ToLower(si.Series.Title), q) || strings.Contains(strings.ToLower(si.Series.SortTitle), q) {
			entries = append(entries, seriesEntry(si))
		}
	}
	start, end := pageBounds(r, len(entries))
	h.paged(w, r, "Search results", "/opds/search?q="+url.QueryEscape(q), entries, start, end)
}

func (h *opdsHandlers) updated(w http.ResponseWriter, r *http.Request) {
	rid, ok := h.identity(w, r)
	if !ok {
		return
	}
	books, err := h.s.deps.Reading.Books(r.Context(), rid, 0)
	if err != nil {
		writeError(w, r, 500, err.Error())
		return
	}
	sort.SliceStable(books, func(i, j int) bool { return books[i].Chapter.UpdatedAt.After(books[j].Chapter.UpdatedAt) })
	entries := []atomEntry{}
	for _, b := range books {
		entries = append(entries, atomEntry{Title: b.Chapter.Title, ID: fmt.Sprintf("urn:mangarr:chapter:%d", b.Chapter.ID), Updated: b.Chapter.UpdatedAt.UTC().Format(time.RFC3339), Links: []atomLink{link("http://opds-spec.org/acquisition/open-access", fmt.Sprintf("/opds/chapters/%d", b.Chapter.ID), "application/vnd.comicbook+zip", "Download CBZ")}})
	}
	start, end := pageBounds(r, len(entries))
	h.paged(w, r, "Recently updated", "/opds/updated", entries, start, end)
}

func (h *opdsHandlers) seriesCover(w http.ResponseWriter, r *http.Request) {
	rid, ok := h.identity(w, r)
	if !ok {
		return
	}
	si, err := h.s.deps.Reading.Series(r.Context(), rid, pathID(r, "id"))
	if err != nil {
		notFoundOr500(w, r, err)
		return
	}
	data, ct, err := h.s.deps.Reading.Cover(r.Context(), &si.Series)
	if err != nil {
		writeError(w, r, 404, "no cover")
		return
	}
	writeImage(w, ct, data)
}

func (h *opdsHandlers) chapterCover(w http.ResponseWriter, r *http.Request) {
	rid, ok := h.identity(w, r)
	if !ok {
		return
	}
	b, err := h.s.deps.Reading.Book(r.Context(), rid, pathID(r, "id"))
	if err != nil {
		notFoundOr500(w, r, err)
		return
	}
	si, err := h.s.deps.Reading.Series(r.Context(), rid, b.Chapter.SeriesID)
	if err != nil {
		notFoundOr500(w, r, err)
		return
	}
	data, ct, err := h.s.deps.Reading.BookThumbnail(r.Context(), b, &si.Series)
	if err != nil {
		writeError(w, r, 404, "no cover")
		return
	}
	writeImage(w, ct, data)
}

// archivePartialMD5 matches KOReader's util.partialMD5: 1 KiB samples at
// 1024 << 2i for i = -1..10. KOReader shifts with LuaJIT's bit.lshift, which
// only uses the low 5 bits of the count, so the i = -1 sample is at offset 0
// (1024 << 30 wraps to 0 in 32 bits), not 256.
func archivePartialMD5(data []byte) string {
	h := md5.New()
	step := int64(1024)
	for i := -1; i <= 10; i++ {
		offset := int64(0)
		if i >= 0 {
			offset = step << uint(2*i)
		}
		if offset >= int64(len(data)) {
			break
		}
		end := offset + 1024
		if end > int64(len(data)) {
			end = int64(len(data))
		}
		_, _ = h.Write(data[offset:end])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (h *opdsHandlers) chapter(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if !p.User.Can(access.Download) {
		writeError(w, r, 403, "your account can't download files")
		return
	}
	b, err := h.s.deps.Reading.Book(r.Context(), p.User.ReaderID, pathID(r, "id"))
	if err != nil {
		notFoundOr500(w, r, err)
		return
	}
	pages, err := h.s.deps.Reading.Pages(r.Context(), b)
	if err != nil {
		pageError(w, r, err)
		return
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i, pg := range pages {
		data, ct, err := h.s.deps.Reading.Page(r.Context(), b, pg.Number)
		if err != nil {
			pageError(w, r, err)
			return
		}
		name := pg.FileName
		if strings.Contains(strings.ToLower(ct), "avif") || strings.Contains(strings.ToLower(ct), "jxl") || strings.HasSuffix(strings.ToLower(name), ".avif") || strings.HasSuffix(strings.ToLower(name), ".jxl") {
			data, ct, err = reading.Convert(data, "jpeg")
			if err != nil {
				writeError(w, r, 502, "couldn't convert a page for KOReader")
				return
			}
			_ = ct
			name = cbz.PageName(i, ".jpg")
		}
		hdr := &zip.FileHeader{Name: name, Method: zip.Store}
		hdr.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		entry, err := zw.CreateHeader(hdr)
		if err != nil {
			writeError(w, r, 500, err.Error())
			return
		}
		if _, err = entry.Write(data); err != nil {
			writeError(w, r, 500, err.Error())
			return
		}
	}
	if err := zw.Close(); err != nil {
		writeError(w, r, 500, err.Error())
		return
	}
	digest := archivePartialMD5(buf.Bytes())
	readerID := p.User.ReaderID
	_, err = h.s.deps.DB.NewInsert().Model(&model.KOReaderDocument{ReaderID: readerID, Document: digest, ChapterID: b.Chapter.ID}).On("CONFLICT (reader_id, document) DO UPDATE").Set("chapter_id = EXCLUDED.chapter_id").Exec(r.Context())
	if err != nil {
		writeError(w, r, 500, err.Error())
		return
	}
	filename := fmt.Sprintf("chapter-%d.cbz", b.Chapter.ID)
	w.Header().Set("Content-Type", "application/vnd.comicbook+zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	_, _ = w.Write(buf.Bytes())
}
