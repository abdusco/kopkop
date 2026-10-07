package imageproc

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/abdusco/kopkop/internal/filesystem"
	"github.com/abdusco/vips-wasm/govips"
	"github.com/stretchr/testify/require"
)

func TestProcessWebPSource(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 200, 100))
	var pngBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, src))
	webpBytes, err := govips.Resize(context.Background(), pngBytes.Bytes(), govips.ResizeOptions{Width: 200, Format: govips.FormatWebP})
	require.NoError(t, err)

	fsys := filesystem.NewMemoryFS()
	require.NoError(t, fsys.WriteFile("a.webp", webpBytes, 0o644))
	p := New(fsys, fsys)
	p.backends = []resizeBackend{{name: "wasm", run: resizeWithWasm}} // no CLI tools, no Go fallback

	res, err := p.Process("a.webp", OpFitWidth, 100, 0)
	require.NoError(t, err)
	require.Equal(t, 100, res.Width)
	require.Equal(t, 50, res.Height)
	out, err := fsys.ReadFile(res.StaticPath)
	require.NoError(t, err)
	conf, format, err := image.DecodeConfig(bytes.NewReader(out))
	require.NoError(t, err)
	require.Equal(t, "webp", format)
	require.Equal(t, 100, conf.Width)
}

func TestResizeWithWasm(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			source.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y * 2), B: 128, A: 255})
		}
	}
	var pngBytes, jpgBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, source))
	require.NoError(t, jpeg.Encode(&jpgBytes, source, nil))

	webpBytes, err := govips.Resize(context.Background(), pngBytes.Bytes(), govips.ResizeOptions{Width: 200, Format: govips.FormatWebP})
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		input   []byte
		ext     string
		w, h    int
		crop    bool
		wantErr string
	}{
		{name: "png same aspect", input: pngBytes.Bytes(), ext: ".png", w: 100, h: 50},
		{name: "jpeg same aspect", input: jpgBytes.Bytes(), ext: ".jpg", w: 40, h: 20},
		{name: "png crop to square", input: pngBytes.Bytes(), ext: ".png", w: 50, h: 50, crop: true},
		{name: "jpeg crop to tall", input: jpgBytes.Bytes(), ext: ".jpg", w: 30, h: 60, crop: true},
		{name: "webp same aspect", input: webpBytes, ext: ".webp", w: 100, h: 50},
		{name: "webp crop", input: webpBytes, ext: ".webp", w: 50, h: 50, crop: true},
		{name: "stretch", input: pngBytes.Bytes(), ext: ".png", w: 50, h: 50},
		{name: "upscale", input: pngBytes.Bytes(), ext: ".png", w: 400, h: 200},
		{name: "upscale crop", input: pngBytes.Bytes(), ext: ".png", w: 400, h: 400, crop: true},
		{name: "gif is unsupported", input: pngBytes.Bytes(), ext: ".gif", w: 100, h: 50, wantErr: "does not support"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := resizeWithWasm(ResizeParams{Input: tc.input, Ext: tc.ext, Width: tc.w, Height: tc.h, Crop: tc.crop})
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			conf, _, err := image.DecodeConfig(bytes.NewReader(out))
			require.NoError(t, err)
			require.Equal(t, tc.w, conf.Width)
			require.Equal(t, tc.h, conf.Height)
		})
	}
}
