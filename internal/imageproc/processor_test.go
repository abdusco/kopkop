package imageproc

import (
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetadataAndResize(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	out := filepath.Join(root, "public")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "images"), 0o755))

	imgPath := filepath.Join(root, "images", "sample.png")
	img := image.NewRGBA(image.Rect(0, 0, 20, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 20; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0, A: 255})
		}
	}
	f, err := os.Create(imgPath)
	require.NoError(t, err)
	require.NoError(t, png.Encode(f, img))
	require.NoError(t, f.Close())

	p := New(root, out)
	md, err := p.GetMetadata("images/sample.png")
	require.NoError(t, err)
	assert.Equal(t, 20, md.Width)
	assert.Equal(t, 10, md.Height)

	url, err := p.Resize("images/sample.png", 8, 4)
	require.NoError(t, err)
	assert.Contains(t, url, "/processed_images/")
	require.FileExists(t, filepath.Join(out, filepath.FromSlash(url[1:])))
}

func TestResizeWithBackends_FallsThroughOnError(t *testing.T) {
	called := make([]string, 0, 3)
	backends := []resizeBackend{
		{
			name: "vips",
			run: func(params ResizeParams) error {
				called = append(called, "vips")
				return errors.New("vips failed")
			},
		},
		{
			name: "magick",
			run: func(params ResizeParams) error {
				called = append(called, "magick")
				return errors.New("magick failed")
			},
		},
		{
			name: "go",
			run: func(params ResizeParams) error {
				called = append(called, "go")
				return nil
			},
		},
	}

	err := resizeWithBackends(backends, ResizeParams{Width: 10, Height: 5})
	require.NoError(t, err)
	assert.Equal(t, []string{"vips", "magick", "go"}, called)
}

func TestResizeWithBackends_ReturnsCombinedErrorWhenAllFail(t *testing.T) {
	backends := []resizeBackend{
		{
			name: "vips",
			run: func(params ResizeParams) error {
				return errors.New("boom1")
			},
		},
		{
			name: "magick",
			run: func(params ResizeParams) error {
				return errors.New("boom2")
			},
		},
	}

	err := resizeWithBackends(backends, ResizeParams{Width: 10, Height: 5})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all resize backends failed")
	assert.Contains(t, err.Error(), "vips: boom1")
	assert.Contains(t, err.Error(), "magick: boom2")
}
