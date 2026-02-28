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
	require.NoError(t, os.MkdirAll(filepath.Join(root, "themes", "hyde", "templates", "shortcodes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "themes", "hyde", "templates", "shortcodes", "pirate.html"), []byte("Arr"), 0o644))

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

	defs := mgr.ShortcodeDefinitions()
	def, ok := defs["pirate"]
	require.True(t, ok)
	assert.Equal(t, "hyde/templates/shortcodes/pirate.html", def.Template)
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

func TestLookupHelpers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "lookup.txt"), []byte(`{% set p = get_page(path="a.md") %}{% set s = get_section(path="blog/_index.md") %}{% set tx = get_taxonomy(kind="tags") %}{{ p.title }}|{{ s.title }}|{{ tx.name }}|{{ get_taxonomy_url(kind="tags", term="Go Lang") }}`), 0o644))

	mgr, err := LoadManager(root, "")
	require.NoError(t, err)

	out, err := mgr.Render("lookup.txt", map[string]any{
		"__pages": map[string]any{
			"a.md": map[string]any{"title": "PageA"},
		},
		"__sections": map[string]any{
			"blog/_index.md": map[string]any{"title": "Blog"},
		},
		"__taxonomies": map[string]any{
			"tags": map[string]any{"name": "tags", "terms": map[string]any{}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "PageA|Blog|tags|/tags/go-lang/", out)
}

func TestNormalizeTemplateSyntax_NamedEndTags(t *testing.T) {
	t.Parallel()

	in := "{% macro twice(str) %}{{str}}{% endmacro twice %}\n{% block a %}x{% endblock a %}\n{{ macros::twice(str=\"hey\") }}"
	out := normalizeTemplateSyntax(in)
	assert.Contains(t, out, "{% endmacro %}")
	assert.Contains(t, out, "{% endblock %}")
	assert.Contains(t, out, "{{ macros.twice(str=\"hey\") }}")
	assert.NotContains(t, out, "endmacro twice")
	assert.NotContains(t, out, "endblock a")
	assert.NotContains(t, out, "macros::twice")
}
