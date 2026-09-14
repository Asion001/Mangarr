package komga

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/Asion001/mangarr/internal/modules/library"
)

type komgaBookMedia struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Media struct {
		Status     string `json:"status"`
		MediaType  string `json:"mediaType"`
		PagesCount int    `json:"pagesCount"`
		Comment    string `json:"comment"`
	} `json:"media"`
}

type komgaPage struct {
	Number    int    `json:"number"`
	MediaType string `json:"mediaType"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

// VerifyBook finds the book for localPath and reports Komga's analysis.
func (m *Module) VerifyBook(ctx context.Context, localPath string) (library.BookCheck, error) {
	var bc library.BookCheck
	remote := m.pm.ToRemote(localPath)
	dir, name := path.Dir(remote), path.Base(remote)
	libs, err := m.libraries(ctx)
	if err != nil {
		return bc, err
	}
	for _, l := range libs {
		if !library.Under(remote, l.Root) {
			continue
		}
		var series pageOf[komgaSeries]
		if err := m.do(ctx, m.s.APIKey, http.MethodPost, "/api/v1/series/list?unpaged=true",
			map[string]any{"condition": is("libraryId", l.ID)}, &series); err != nil {
			return bc, fmt.Errorf("list series: %w", err)
		}
		for _, s := range series.Content {
			if path.Clean(strings.ReplaceAll(s.URL, "\\", "/")) != dir {
				continue
			}
			var books pageOf[komgaBookMedia]
			if err := m.do(ctx, m.s.APIKey, http.MethodGet, "/api/v1/series/"+s.ID+"/books?unpaged=true", nil, &books); err != nil {
				return bc, fmt.Errorf("list books: %w", err)
			}
			for _, b := range books.Content {
				if path.Base(strings.ReplaceAll(b.URL, "\\", "/")) != name {
					continue
				}
				bc.Found, bc.Status, bc.Pages = true, b.Media.Status, b.Media.PagesCount
				switch b.Media.Status {
				case "READY":
				case "ERROR", "UNSUPPORTED":
					bc.Problem = strings.TrimSpace("Komga could not analyze the file: " + b.Media.Status + " " + b.Media.Comment)
					return bc, nil
				default:
					return bc, nil // not analyzed yet
				}
				var pages []komgaPage
				if err := m.do(ctx, m.s.APIKey, http.MethodGet, "/api/v1/books/"+b.ID+"/pages", nil, &pages); err != nil {
					return bc, fmt.Errorf("book pages: %w", err)
				}
				if len(pages) > 0 {
					bc.PageWidth = pages[0].Width
					if bc.PageWidth == 0 {
						bc.Problem = fmt.Sprintf("Komga can't read %s pages (no dimensions); its image libraries may lack support for this format", pages[0].MediaType)
					}
				}
				return bc, nil
			}
		}
	}
	return bc, nil
}

var _ library.Verifier = (*Module)(nil)
