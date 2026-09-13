// Package ncnnworker implements the upscale module that talks to a
// mangarr-upscaler worker over HTTP.
package ncnnworker

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/httpx"
	"github.com/Asion001/mangarr/internal/modules/upscale"
)

type Settings struct {
	URL     string `json:"url" label:"Worker URL" type:"url" required:"true" placeholder:"http://mangarr-upscaler:8788" order:"1"`
	Token   string `json:"token" label:"Token" secret:"true" order:"2" help:"Value of UPSCALER_TOKEN on the worker."`
	Timeout int    `json:"timeout" label:"Timeout per chapter (min)" order:"3" advanced:"true"`
}

func init() {
	modules.Register(&modules.Implementation{
		Kind: modules.KindUpscale, Name: "ncnn-worker", DisplayName: "mangarr-upscaler (ncnn/Vulkan)",
		Description: "Upscales pages with waifu2x, Real-CUGAN or Real-ESRGAN on a mangarr-upscaler worker (local iGPU or a GPU machine).",
		Settings:    func() any { return &Settings{Timeout: 30} },
		New: func(deps modules.Deps, s any) (modules.Instance, error) {
			st := s.(*Settings)
			timeout := time.Duration(st.Timeout) * time.Minute
			if timeout <= 0 {
				timeout = 30 * time.Minute
			}
			hc := &http.Client{Timeout: timeout}
			if deps.HTTP != nil {
				hc.Transport = deps.HTTP.Transport
			}
			return &Module{s: st, http: hc}, nil
		},
	})
}

type Module struct {
	s    *Settings
	http *http.Client

	mu     sync.Mutex
	info   *upscale.Info
	infoAt time.Time
}

func (m *Module) headers() map[string]string {
	if m.s.Token == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + m.s.Token}
}

func (m *Module) Test(ctx context.Context) error {
	info, err := m.fetchInfo(ctx)
	if err != nil {
		return err
	}
	if len(info.Models) == 0 {
		return fmt.Errorf("worker has no upscaler models installed")
	}
	return nil
}

func (m *Module) fetchInfo(ctx context.Context) (*upscale.Info, error) {
	var info upscale.Info
	if err := httpx.Do(ctx, m.http, http.MethodGet, httpx.Join(m.s.URL, "/v1/info"), m.headers(), nil, &info); err != nil {
		return nil, fmt.Errorf("upscaler worker: %w", err)
	}
	m.mu.Lock()
	m.info, m.infoAt = &info, time.Now()
	m.mu.Unlock()
	return &info, nil
}

// Info returns cached worker info (refreshed every 5 minutes).
func (m *Module) Info(ctx context.Context) (*upscale.Info, error) {
	m.mu.Lock()
	info, at := m.info, m.infoAt
	m.mu.Unlock()
	if info != nil && time.Since(at) < 5*time.Minute {
		return info, nil
	}
	return m.fetchInfo(ctx)
}

func (m *Module) Upscale(ctx context.Context, images []upscale.Image, p upscale.Params) ([]upscale.Image, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, img := range images {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: img.Name, Method: zip.Store})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(img.Data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("model", p.Model)
	q.Set("scale", strconv.Itoa(p.Scale))
	q.Set("noise", strconv.Itoa(p.Noise))
	q.Set("format", p.Format)
	q.Set("quality", strconv.Itoa(p.Quality))
	if p.MaxWidth > 0 {
		q.Set("maxWidth", strconv.Itoa(p.MaxWidth))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, httpx.Join(m.s.URL, "/v1/upscale")+"?"+q.Encode(), &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/zip")
	for k, v := range m.headers() {
		req.Header.Set(k, v)
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upscaler worker: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<30))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upscaler worker: HTTP %d: %s", resp.StatusCode, httpx.Snippet(body))
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("upscaler worker returned an invalid zip: %w", err)
	}
	byBase := map[string]upscale.Image{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		byBase[strings.TrimSuffix(f.Name, filepath.Ext(f.Name))] = upscale.Image{Name: f.Name, Data: data}
	}
	out := make([]upscale.Image, 0, len(images))
	for _, in := range images {
		o, ok := byBase[strings.TrimSuffix(in.Name, filepath.Ext(in.Name))]
		if !ok {
			return nil, fmt.Errorf("upscaler worker did not return %s", in.Name)
		}
		out = append(out, o)
	}
	return out, nil
}
