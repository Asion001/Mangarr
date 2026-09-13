// Package backup creates zip backups of the database (SQLite) and settings.
package backup

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/settings"
	"github.com/Asion001/mangarr/internal/version"
)

type Backup struct {
	Name    string    `json:"name"`
	Type    string    `json:"type"` // manual | scheduled
	Size    int64     `json:"size"`
	Created time.Time `json:"created"`
}

type Service struct {
	db       *db.DB
	settings *settings.Store
	dir      string
}

func New(d *db.DB, st *settings.Store, dataDir string) *Service {
	return &Service{db: d, settings: st, dir: filepath.Join(dataDir, "backups")}
}

func (s *Service) Dir() string { return s.dir }

// Create writes a new backup and applies retention to scheduled backups.
func (s *Service) Create(ctx context.Context, typ string) (*Backup, error) {
	if err := os.MkdirAll(s.dir, 0o775); err != nil {
		return nil, err
	}
	name := fmt.Sprintf("mangarr_%s_%s.zip", typ, time.Now().UTC().Format("2006.01.02_15.04.05"))
	path := filepath.Join(s.dir, name)
	tmp := path + ".partial"
	f, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}
	zw := zip.NewWriter(f)
	fail := func(err error) (*Backup, error) {
		_ = zw.Close()
		_ = f.Close()
		_ = os.Remove(tmp)
		return nil, err
	}
	manifest := map[string]any{"version": version.Version, "database": string(s.db.Kind), "created": time.Now().UTC()}
	if s.db.Kind == db.SQLite {
		dbTmp := filepath.Join(s.dir, ".backup-db.tmp")
		if err := s.db.Backup(ctx, dbTmp); err != nil {
			return fail(err)
		}
		err := addFile(zw, "mangarr.db", dbTmp)
		_ = os.Remove(dbTmp)
		if err != nil {
			return fail(err)
		}
	} else {
		manifest["note"] = "PostgreSQL data is not included; back it up with pg_dump. Settings and modules are exported as JSON."
		var rows []model.Setting
		if err := s.db.NewSelect().Model(&rows).Scan(ctx); err != nil {
			return fail(err)
		}
		if err := addJSON(zw, "settings.json", rows); err != nil {
			return fail(err)
		}
		var defs []model.ProviderDefinition
		if err := s.db.NewSelect().Model(&defs).Scan(ctx); err != nil {
			return fail(err)
		}
		if err := addJSON(zw, "modules.json", defs); err != nil {
			return fail(err)
		}
	}
	if err := addJSON(zw, "manifest.json", manifest); err != nil {
		return fail(err)
	}
	if err := zw.Close(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return nil, err
	}
	if typ == "scheduled" {
		g, _ := s.settings.General(ctx)
		s.prune(g.BackupRetention)
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &Backup{Name: name, Type: typ, Size: st.Size(), Created: st.ModTime()}, nil
}

func addFile(zw *zip.Writer, name, path string) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, in)
	return err
}

func addJSON(zw *zip.Writer, name string, v any) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// List returns backups, newest first.
func (s *Service) List() ([]Backup, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Backup{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Backup{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		typ := "manual"
		if strings.Contains(e.Name(), "_scheduled_") {
			typ = "scheduled"
		}
		out = append(out, Backup{Name: e.Name(), Type: typ, Size: info.Size(), Created: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

// Path returns the absolute path of a backup, rejecting path traversal.
func (s *Service) Path(name string) (string, error) {
	if name != filepath.Base(name) || !strings.HasSuffix(name, ".zip") {
		return "", errors.New("invalid backup name")
	}
	p := filepath.Join(s.dir, name)
	if _, err := os.Stat(p); err != nil {
		return "", err
	}
	return p, nil
}

func (s *Service) Delete(name string) error {
	p, err := s.Path(name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

func (s *Service) prune(keep int) {
	if keep <= 0 {
		keep = 7
	}
	list, err := s.List()
	if err != nil {
		return
	}
	n := 0
	for _, b := range list {
		if b.Type != "scheduled" {
			continue
		}
		n++
		if n > keep {
			_ = os.Remove(filepath.Join(s.dir, b.Name))
		}
	}
}
