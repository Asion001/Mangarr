// Package upscaler is the mangarr-upscaler worker: a small HTTP service that
// upscales batches of pages with the ncnn/Vulkan CLIs (waifu2x, Real-CUGAN,
// Real-ESRGAN). It runs next to mangarr (Intel/AMD iGPU through /dev/dri) or
// on any machine with a GPU.
package upscaler

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// Engine describes one model of one ncnn tool.
type Engine struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Tool        string `json:"-"` // folder under ToolsDir
	Binary      string `json:"-"`
	ModelDir    string `json:"-"`
	ModelName   string `json:"-"` // realesrgan -n
	Scales      []int  `json:"scales"`
	NoiseLevels []int  `json:"noiseLevels,omitempty"`
}

// Catalog lists every model the worker knows. Availability depends on which
// tools are installed.
var Catalog = []Engine{
	{Name: "waifu2x-cunet", Description: "waifu2x CUnet – clean line art, good for black & white manga; removes JPEG noise",
		Tool: "waifu2x", Binary: "waifu2x-ncnn-vulkan", ModelDir: "models-cunet", Scales: []int{2, 4, 8}, NoiseLevels: []int{-1, 0, 1, 2, 3}},
	{Name: "waifu2x-anime", Description: "waifu2x UpConv7 anime style – fast, color pages",
		Tool: "waifu2x", Binary: "waifu2x-ncnn-vulkan", ModelDir: "models-upconv_7_anime_style_art_rgb", Scales: []int{2, 4, 8}, NoiseLevels: []int{-1, 0, 1, 2, 3}},
	{Name: "realcugan", Description: "Real-CUGAN SE – sharp anime/manga art",
		Tool: "realcugan", Binary: "realcugan-ncnn-vulkan", ModelDir: "models-se", Scales: []int{2, 3, 4}, NoiseLevels: []int{-1, 0, 1, 2, 3}},
	{Name: "realesr-animevideov3", Description: "Real-ESRGAN AnimeVideo v3 – fast, good for color webtoons",
		Tool: "realesrgan", Binary: "realesrgan-ncnn-vulkan", ModelDir: "models", ModelName: "realesr-animevideov3", Scales: []int{2, 3, 4}},
	{Name: "realesrgan-x4plus-anime", Description: "Real-ESRGAN x4plus anime – slow, strongest restoration",
		Tool: "realesrgan", Binary: "realesrgan-ncnn-vulkan", ModelDir: "models", ModelName: "realesrgan-x4plus-anime", Scales: []int{4}},
}

func findEngine(name string) (Engine, bool) {
	for _, e := range Catalog {
		if e.Name == name {
			return e, true
		}
	}
	return Engine{}, false
}

// Runner executes an engine on a directory of images (outputs PNG files
// named after the inputs).
type Runner interface {
	Available(e Engine) bool
	Run(ctx context.Context, e Engine, inDir, outDir string, scale, noise int) error
}

// CLIRunner runs the real ncnn binaries from ToolsDir/<tool>/.
type CLIRunner struct {
	ToolsDir string
	GPU      string // "" = auto
	Threads  string // e.g. "1:2:2"
	Tile     int
}

func (r CLIRunner) bin(e Engine) string { return filepath.Join(r.ToolsDir, e.Tool, e.Binary) }

func (r CLIRunner) Available(e Engine) bool {
	st, err := os.Stat(r.bin(e))
	if err != nil || st.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(r.ToolsDir, e.Tool, e.ModelDir))
	return err == nil
}

func (r CLIRunner) Run(ctx context.Context, e Engine, inDir, outDir string, scale, noise int) error {
	args := []string{"-i", inDir, "-o", outDir, "-s", strconv.Itoa(scale), "-f", "png",
		"-m", filepath.Join(r.ToolsDir, e.Tool, e.ModelDir)}
	if e.ModelName != "" {
		args = append(args, "-n", e.ModelName)
	} else if len(e.NoiseLevels) > 0 {
		args = append(args, "-n", strconv.Itoa(noise))
	}
	if r.GPU != "" && r.GPU != "auto" {
		args = append(args, "-g", r.GPU)
	}
	if r.Threads != "" {
		args = append(args, "-j", r.Threads)
	}
	if r.Tile > 0 {
		args = append(args, "-t", strconv.Itoa(r.Tile))
	}
	cmd := exec.CommandContext(ctx, r.bin(e), args...)
	cmd.Dir = filepath.Join(r.ToolsDir, e.Tool)
	out, err := cmd.CombinedOutput()
	if err != nil {
		tail := string(out)
		if len(tail) > 2000 {
			tail = tail[len(tail)-2000:]
		}
		return fmt.Errorf("%s failed: %w: %s", e.Binary, err, tail)
	}
	return nil
}
