package templates

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

func TestManagerLoadAndRenderFallbacks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "themes", "hyde", "templates"), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(root, "themes", "hyde", "templates", "page.html"), []byte("theme-page"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "section.html"), []byte("site-section"), 0o644))

	mgr, err := LoadManager(root, "hyde")
	require.NoError(t, err)

	out, err := mgr.Render("page.html", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "theme-page", out)

	out, err = mgr.Render("section.html", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "site-section", out)

	out, err = mgr.Render("404.html", map[string]any{})
	require.NoError(t, err)
	assert.Contains(t, out, "404")
}

func TestManagerHelpers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "images"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "data.txt"), []byte("hello"), 0o644))
	img := image.NewRGBA(image.Rect(0, 0, 10, 5))
	for y := 0; y < 5; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	imgFile, err := os.Create(filepath.Join(root, "images", "sample.png"))
	require.NoError(t, err)
	require.NoError(t, png.Encode(imgFile, img))
	require.NoError(t, imgFile.Close())
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "test.txt"), []byte(`{{ "abc"|base64_encode }}|{{ "YWJj"|base64_decode }}|{{ "a1b2"|regex_replace("[0-9]", "") }}|{{ get_url("posts/hello") }}|{{ load_data("data.txt") }}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "test-image.txt"), []byte(`{% set md = get_image_metadata("images/sample.png") %}{{ md.width }}x{{ md.height }}|{{ resize_image("images/sample.png", 4, 2) }}`), 0o644))

	mgr, err := LoadManager(root, "")
	require.NoError(t, err)

	out, err := mgr.Render("test.txt", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "YWJj|abc|ab|/posts/hello|hello", out)

	imgOut, err := mgr.Render("test-image.txt", map[string]any{})
	require.NoError(t, err)
	assert.Contains(t, imgOut, "10x5|")
	assert.Contains(t, imgOut, "/processed_images/")
}
