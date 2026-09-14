package komga

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/Asion001/mangarr/internal/modules/library"
)

// bookIDs maps local file paths to Komga book ids (admin key).
func (m *Module) bookIDs(ctx context.Context, localPaths []string) (map[string]string, error) {
	wantDir := map[string]bool{}
	for _, lp := range localPaths {
		wantDir[path.Dir(m.pm.ToRemote(lp))] = true
	}
	libs, err := m.libraries(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, l := range libs {
		inLib := false
		for d := range wantDir {
			if library.Under(d, l.Root) {
				inLib = true
			}
		}
		if !inLib {
			continue
		}
		var series pageOf[komgaSeries]
		if err := m.do(ctx, m.s.APIKey, http.MethodPost, "/api/v1/series/list?unpaged=true",
			map[string]any{"condition": is("libraryId", l.ID)}, &series); err != nil {
			return nil, fmt.Errorf("list series: %w", err)
		}
		for _, s := range series.Content {
			dir := path.Clean(strings.ReplaceAll(s.URL, "\\", "/"))
			if !wantDir[dir] {
				continue
			}
			var books pageOf[komgaBookMedia]
			if err := m.do(ctx, m.s.APIKey, http.MethodGet, "/api/v1/series/"+s.ID+"/books?unpaged=true", nil, &books); err != nil {
				return nil, fmt.Errorf("list books: %w", err)
			}
			for _, b := range books.Content {
				remote := path.Join(dir, path.Base(strings.ReplaceAll(b.URL, "\\", "/")))
				out[remote] = b.ID
			}
		}
	}
	return out, nil
}

// WriteProgress marks books read (or sets the page) for the reader's account.
func (m *Module) WriteProgress(ctx context.Context, acc library.Account, items []library.BookProgress) (int, []string, error) {
	paths := make([]string, len(items))
	for i, it := range items {
		paths[i] = it.LocalPath
	}
	ids, err := m.bookIDs(ctx, paths)
	if err != nil {
		return 0, nil, err
	}
	written := 0
	var missing []string
	for _, it := range items {
		id, ok := ids[m.pm.ToRemote(it.LocalPath)]
		if !ok {
			missing = append(missing, it.LocalPath)
			continue
		}
		body := map[string]any{"completed": it.Completed}
		if !it.Completed && it.Page > 0 {
			body = map[string]any{"page": it.Page}
		}
		if err := m.do(ctx, acc.Credentials["apiKey"], http.MethodPatch, "/api/v1/books/"+id+"/read-progress", body, nil); err != nil {
			return written, missing, fmt.Errorf("set progress: %w", err)
		}
		written++
	}
	return written, missing, nil
}

var _ library.ProgressWriter = (*Module)(nil)
