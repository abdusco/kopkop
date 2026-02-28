package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSiteBuild_Minimal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "zola.toml"), []byte(`
base_url = "https://example.com"
title = "Demo"
output_dir = "public"
compile_sass = false
generate_sitemap = true
generate_feeds = false
build_search_index = false
generate_robots_txt = true
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\n+++\nWelcome"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "post.md"), []byte("+++\ntitle='Post'\n+++\nHello world"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "page.html"), []byte("<html><body>{{ page.content|safe }}</body></html>"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "section.html"), []byte("<html><body>{{ section.title }}</body></html>"), 0o644))

	s, err := New(root, filepath.Join(root, "zola.toml"))
	require.NoError(t, err)
	require.NoError(t, s.Load(false))
	require.NoError(t, s.Build(BuildOptions{BuildMode: BuildDisk, Force: true}))

	_, err = os.Stat(filepath.Join(root, "public", "post", "index.html"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "public", "sitemap.xml"))
	require.NoError(t, err)
}

func TestMinifyHTML(t *testing.T) {
	t.Parallel()

	in := "<html>\n  <body>  <h1> Hi </h1> </body>\n</html>"
	out := minifyHTML(in)
	require.Equal(t, "<html><body><h1> Hi </h1></body></html>", out)
}
