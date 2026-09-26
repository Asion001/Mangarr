package processing

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Asion001/mangarr/internal/downloads"
	"github.com/Asion001/mangarr/internal/model"
)

func TestInsideRefusesEscapes(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{"", ".", "..", "../x.png", "a/../../x.png", "/etc/passwd"} {
		if _, err := inside(dir, bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	got, err := inside(dir, "upscaled/0001.png")
	if err != nil || got != filepath.Join(dir, "upscaled", "0001.png") {
		t.Fatalf("got %q, %v", got, err)
	}
}

type fixed struct{ name string }

func (f fixed) Process(ctx context.Context, cfg model.ProfileConfig, pages []downloads.PageFile, workDir string) (downloads.ProcessResult, error) {
	return downloads.ProcessResult{Encoder: f.name}, nil
}

func TestSwitchFollowsTheSetting(t *testing.T) {
	remote := false
	s := &Switch{Local: fixed{"local"}, Remote: fixed{"remote"}, UseRemote: func(context.Context) bool { return remote }}
	for _, want := range []string{"local", "remote"} {
		remote = want == "remote"
		res, _ := s.Process(context.Background(), model.ProfileConfig{}, nil, "")
		if res.Encoder != want {
			t.Fatalf("got %s, want %s", res.Encoder, want)
		}
	}
}
