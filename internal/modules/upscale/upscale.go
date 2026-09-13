// Package upscale defines upscaler modules. The v1 implementation talks to a
// mangarr-upscaler worker (ncnn/Vulkan); other backends (PyTorch/ONNX
// MangaJaNai, cloud APIs) can be added as separate modules.
package upscale

import (
	"context"

	"github.com/Asion001/mangarr/internal/modules"
)

type Model struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Scales      []int  `json:"scales"`
	NoiseLevels []int  `json:"noiseLevels,omitempty"`
}

type Info struct {
	Version string   `json:"version"`
	Devices []string `json:"devices"`
	Models  []Model  `json:"models"`
}

type Params struct {
	Model    string `json:"model"`
	Scale    int    `json:"scale"`
	Noise    int    `json:"noise"`
	Format   string `json:"format"` // webp, jpeg, png
	Quality  int    `json:"quality"`
	MaxWidth int    `json:"maxWidth"` // 0 = no cap
}

// Image is an in-memory page.
type Image struct {
	Name string
	Data []byte
}

type Module interface {
	modules.Instance
	Info(ctx context.Context) (*Info, error)
	// Upscale processes a batch of images with the same parameters. Output
	// names keep the input base name with the new format's extension.
	Upscale(ctx context.Context, images []Image, p Params) ([]Image, error)
}
