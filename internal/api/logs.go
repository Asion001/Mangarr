package api

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/logging"
	"github.com/Asion001/mangarr/internal/redact"
)

func init() { register((*Server).registerLogs) }

type LogFiles struct {
	// Enabled is false when log files are turned off (MANGARR_LOG_DIR=off).
	Enabled bool              `json:"enabled"`
	Dir     string            `json:"dir"`
	Files   []logging.LogFile `json:"files"`
}

type downloadOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	Body               []byte
}

// writeLogs adds the redacted log files (or, without log files, the recent
// in-memory entries) to a zip under prefix.
func (s *Server) writeLogs(zw *zip.Writer, prefix string, red *redact.Redactor) error {
	files := []logging.LogFile{}
	if s.app.Cfg.LogDir != "" {
		files = logging.Files(s.app.Cfg.LogDir)
	}
	if len(files) == 0 {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: prefix + "recent.txt", Method: zip.Deflate, Modified: time.Now()})
		if err != nil {
			return err
		}
		var buf strings.Builder
		for _, e := range s.app.LogRing.Entries(logging.ParseLevel("debug"), 2000) {
			fmt.Fprintf(&buf, "%s %s %s", e.Time.Format(time.RFC3339), e.Level, e.Message)
			for k, v := range e.Attrs {
				fmt.Fprintf(&buf, " %s=%v", k, v)
			}
			buf.WriteByte('\n')
		}
		return red.Copy(w, strings.NewReader(buf.String()))
	}
	for _, f := range files {
		src, err := os.Open(filepath.Join(s.app.Cfg.LogDir, f.Name))
		if err != nil {
			continue
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: prefix + f.Name, Method: zip.Deflate, Modified: f.Modified})
		if err == nil {
			err = red.Copy(w, src)
		}
		src.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) registerLogs() {
	tags := []string{"System"}
	huma.Register(s.api, huma.Operation{OperationID: "system-log-files", Method: http.MethodGet, Path: "/api/v1/system/logs/files", Tags: tags},
		func(ctx context.Context, _ *struct{}) (*struct{ Body LogFiles }, error) {
			out := LogFiles{Enabled: s.app.Cfg.LogDir != "", Dir: s.app.Cfg.LogDir, Files: []logging.LogFile{}}
			if out.Enabled {
				out.Files = logging.Files(s.app.Cfg.LogDir)
			}
			return &struct{ Body LogFiles }{out}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "system-logs-download", Method: http.MethodGet, Path: "/api/v1/system/logs/download", Tags: tags,
		Summary: "Download log files with secrets removed: one file as text, or all as a zip"},
		func(ctx context.Context, in *struct {
			File string `query:"file" doc:"One log file (from /system/logs/files); empty = all as a zip"`
		}) (*downloadOutput, error) {
			red := s.app.Redactor(ctx)
			if in.File != "" {
				name := filepath.Base(in.File)
				if s.app.Cfg.LogDir == "" || name != in.File || !strings.HasSuffix(name, ".txt") {
					return nil, huma.Error404NotFound("no such log file")
				}
				f, err := os.Open(filepath.Join(s.app.Cfg.LogDir, name))
				if err != nil {
					return nil, huma.Error404NotFound("no such log file")
				}
				defer f.Close()
				var buf bytes.Buffer
				if err := red.Copy(&buf, f); err != nil {
					return nil, toHTTPError(err)
				}
				return &downloadOutput{ContentType: "text/plain; charset=utf-8", ContentDisposition: `attachment; filename="` + name + `"`, Body: buf.Bytes()}, nil
			}
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			if err := s.writeLogs(zw, "", red); err != nil {
				return nil, toHTTPError(err)
			}
			if err := zw.Close(); err != nil {
				return nil, toHTTPError(err)
			}
			name := "mangarr-logs-" + time.Now().Format("20060102-150405") + ".zip"
			return &downloadOutput{ContentType: "application/zip", ContentDisposition: `attachment; filename="` + name + `"`, Body: buf.Bytes()}, nil
		})
}
