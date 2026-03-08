package imageproc

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/abdusco/kopkop/internal/filesystem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetadataAndResize(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
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

	p := New(filesystem.NewDiskFS(root), filesystem.NewDiskFS(root))
	md, err := p.GetMetadata("images/sample.png")
	require.NoError(t, err)
	assert.Equal(t, 20, md.Width)
	assert.Equal(t, 10, md.Height)

	url, err := p.Resize("images/sample.png", 8, 4)
	require.NoError(t, err)
	assert.Contains(t, url, "/processed_images/")
	require.FileExists(t, filepath.Join(root, filepath.FromSlash(url[1:])))
}

func TestResizeWithBackends_FallsThroughOnError(t *testing.T) {
	called := make([]string, 0, 3)
	input := []byte("input")
	backends := []resizeBackend{
		{
			name: "vips",
			run: func(params ResizeParams) ([]byte, error) {
				called = append(called, "vips")
				return nil, errors.New("vips failed")
			},
		},
		{
			name: "magick",
			run: func(params ResizeParams) ([]byte, error) {
				called = append(called, "magick")
				return nil, errors.New("magick failed")
			},
		},
		{
			name: "go",
			run: func(params ResizeParams) ([]byte, error) {
				called = append(called, "go")
				return []byte("ok"), nil
			},
		},
	}

	out, err := resizeWithBackends(backends, ResizeParams{Input: input, Ext: ".png", Width: 10, Height: 5})
	require.NoError(t, err)
	assert.Equal(t, []byte("ok"), out)
	assert.Equal(t, []string{"vips", "magick", "go"}, called)
}

func TestResizeWithBackends_ReturnsCombinedErrorWhenAllFail(t *testing.T) {
	backends := []resizeBackend{
		{
			name: "vips",
			run: func(params ResizeParams) ([]byte, error) {
				return nil, errors.New("boom1")
			},
		},
		{
			name: "magick",
			run: func(params ResizeParams) ([]byte, error) {
				return nil, errors.New("boom2")
			},
		},
	}

	_, err := resizeWithBackends(backends, ResizeParams{Input: []byte("input"), Ext: ".png", Width: 10, Height: 5})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all resize backends failed")
	assert.Contains(t, err.Error(), "vips: boom1")
	assert.Contains(t, err.Error(), "magick: boom2")
}

func TestResize_WritesViaProvidedFS(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 6, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 6; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var pngBuf bytes.Buffer
	require.NoError(t, png.Encode(&pngBuf, img))

	mfs := filesystem.NewMemoryFS()
	require.NoError(t, mfs.WriteFile("images/sample.png", pngBuf.Bytes(), 0o644))
	p := New(mfs, mfs)

	url, err := p.Resize("images/sample.png", 3, 2)
	require.NoError(t, err)
	assert.Equal(t, "/processed_images/sample-3x2.png", url)

	out, err := mfs.ReadFile("processed_images/sample-3x2.png")
	require.NoError(t, err)
	conf, _, err := image.DecodeConfig(bytes.NewReader(out))
	require.NoError(t, err)
	assert.Equal(t, 3, conf.Width)
	assert.Equal(t, 2, conf.Height)
}
