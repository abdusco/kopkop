package imageproc

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"strings"

	"github.com/abdusco/vips-wasm/govips"
)

const wasmQuality = 85

// resizeWithWasm runs libvips compiled to WASM. The output size is verified so
// a backend that gets it wrong falls through to the next one.
func resizeWithWasm(params ResizeParams) ([]byte, error) {
	var format govips.Format
	switch strings.ToLower(params.Ext) {
	case ".jpg", ".jpeg":
		format = govips.FormatJPEG
	case ".png":
		format = govips.FormatPNG
	case ".webp":
		format = govips.FormatWebP
	default:
		return nil, fmt.Errorf("wasm resize backend does not support format: %s", params.Ext)
	}

	mode := govips.ModeForce
	if params.Crop {
		mode = govips.ModeCrop
	}
	out, err := govips.Resize(context.Background(), params.Input, govips.ResizeOptions{
		Width:   params.Width,
		Height:  params.Height,
		Format:  format,
		Mode:    mode,
		Quality: wasmQuality,
	})
	if err != nil {
		return nil, err
	}

	conf, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		return nil, err
	}
	if conf.Width != params.Width || conf.Height != params.Height {
		return nil, fmt.Errorf("produced %dx%d, want %dx%d", conf.Width, conf.Height, params.Width, params.Height)
	}
	return out, nil
}
