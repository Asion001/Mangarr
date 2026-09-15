package komgaapi

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/reading"
)

func id(n int64) string { return strconv.FormatInt(n, 10) }

// seriesStatus maps mangarr's status to Komga's.
func seriesStatus(s string) string {
	switch s {
	case model.StatusCompleted:
		return "ENDED"
	case model.StatusHiatus:
		return "HIATUS"
	case model.StatusCancelled:
		return "ABANDONED"
	}
	return "ONGOING"
}

func readingDirection(d string) string {
	switch d {
	case "ltr":
		return "LEFT_TO_RIGHT"
	case "vertical":
		return "VERTICAL"
	case "webtoon":
		return "WEBTOON"
	}
	return "RIGHT_TO_LEFT"
}

// readStatus is a series' status for the reader (READ, UNREAD, IN_PROGRESS).
func seriesReadStatus(si reading.SeriesInfo) string {
	switch {
	case si.Books > 0 && si.Read >= si.Books:
		return "READ"
	case si.Read == 0 && si.InProgress == 0:
		return "UNREAD"
	}
	return "IN_PROGRESS"
}

func bookReadStatus(b reading.BookInfo) string {
	switch {
	case b.State == nil || (!b.State.Completed && b.State.Page == 0):
		return "UNREAD"
	case b.State.Completed:
		return "READ"
	}
	return "IN_PROGRESS"
}

func day(t *time.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	s := t.UTC().Format("2006-01-02")
	return &s
}

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func seriesAuthors(md model.SeriesMetadata) []authorDTO {
	out := []authorDTO{}
	for _, a := range md.Authors {
		out = append(out, authorDTO{Name: a, Role: "writer"})
	}
	for _, a := range md.Artists {
		out = append(out, authorDTO{Name: a, Role: "penciller"})
	}
	return out
}

func toSeries(si reading.SeriesInfo) seriesDTO {
	ser := si.Series
	md := ser.Metadata
	created, modified := komgaTime(ser.AddedAt), komgaTime(si.LastModified())
	m := seriesMetadataDTO{
		Status: seriesStatus(ser.Status), Title: ser.Title, TitleSort: ser.SortTitle, Summary: md.Description,
		ReadingDirection: readingDirection(ser.ReadingDirection), Publisher: md.Publisher, Language: ser.Language,
		Genres: nonNil(md.Genres), Tags: nonNil(md.Tags), SharingLabels: []string{}, Links: []webLinkDTO{},
		AlternateTitles: []alternateTitleDTO{}, Created: created, LastModified: modified,
	}
	if md.TotalChapters > 0 {
		n := md.TotalChapters
		m.TotalBookCount = &n
	}
	for label, u := range md.Links {
		m.Links = append(m.Links, webLinkDTO{Label: label, URL: u})
	}
	for _, t := range md.AltTitles {
		m.AlternateTitles = append(m.AlternateTitles, alternateTitleDTO{Label: "", Title: t})
	}
	return seriesDTO{
		ID: id(ser.ID), LibraryID: id(ser.RootFolderID), Name: ser.Title, URL: si.Dir, Created: created, LastModified: modified,
		FileLastModified: modified, BooksCount: si.Books, BooksReadCount: si.Read, BooksUnreadCount: si.Unread(),
		BooksInProgressCount: si.InProgress, Metadata: m,
		BooksMetadata: booksMetadataDTO{Authors: seriesAuthors(md), Tags: []string{}, ReleaseDate: day(si.FirstRelease),
			Summary: "", SummaryNumber: "", Created: created, LastModified: modified},
	}
}

// humanSize formats sizes like Komga ("12.3 MiB").
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// chapterTitle names a chapter ("Chapter 12" when the source gave no title).
func chapterTitle(ch model.Chapter) string {
	if t := strings.TrimSpace(ch.Title); t != "" {
		return t
	}
	return "Chapter " + ch.NumberKey
}

// toBook maps a chapter. pages is the known page count (0 = unknown yet).
func toBook(b reading.BookInfo, ser *model.Series, seriesDir string, pages int) bookDTO {
	ch := b.Chapter
	created, modified := ch.FirstSeenAt, ch.UpdatedAt
	title := chapterTitle(ch)
	release := ch.ReleaseDate
	if release == nil {
		release = &ch.FirstSeenAt
	}
	out := bookDTO{
		ID: id(ch.ID), SeriesID: id(ch.SeriesID), SeriesTitle: ser.Title, LibraryID: id(ser.RootFolderID), Name: title,
		Number: float64(b.Index), Created: komgaTime(created), LastModified: komgaTime(modified), FileLastModified: komgaTime(modified),
		Size: "not downloaded", URL: filepath.Join(seriesDir, title),
		Media: mediaDTO{Status: "READY", MediaType: "application/zip", MediaProfile: "DIVINA", PagesCount: pages},
		Metadata: bookMetadataDTO{Title: title, Summary: "", Number: ch.NumberKey, NumberSort: ch.NumberSort, ReleaseDate: day(release),
			Authors: []authorDTO{}, Tags: []string{}, Links: []webLinkDTO{}, Created: komgaTime(created), LastModified: komgaTime(modified)},
	}
	if b.Scanlator != "" {
		out.Metadata.Authors = append(out.Metadata.Authors, authorDTO{Name: b.Scanlator, Role: "translator"})
	}
	if b.File != nil {
		out.SizeBytes, out.Size = b.File.Size, humanSize(b.File.Size)
		out.FileLastModified = komgaTime(b.File.ImportedAt)
		if b.File.ImportedAt.After(modified) {
			out.LastModified = komgaTime(b.File.ImportedAt)
		}
		if b.Path != "" {
			out.URL = b.Path // ends in .cbz: KMReader downloads it through /file
		}
		if out.Media.PagesCount == 0 {
			out.Media.PagesCount = b.File.PageCount
		}
	}
	if st := b.State; st != nil && (st.Completed || st.Page > 0) {
		at := st.SyncedAt
		if st.ReadAt != nil {
			at = *st.ReadAt
		}
		out.ReadProgress = &readProgressDTO{Page: st.Page, Completed: st.Completed, ReadDate: komgaTime(at), Created: komgaTime(at),
			LastModified: komgaTime(st.SyncedAt)}
		if st.Completed && out.ReadProgress.Page == 0 {
			out.ReadProgress.Page = max(out.Media.PagesCount, 1)
		}
	}
	return out
}
