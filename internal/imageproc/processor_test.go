package imageproc

import (
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
