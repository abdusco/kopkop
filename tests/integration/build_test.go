package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/abdusco/kopkop/tests/harness"
)

func TestBuild_IsDeterministicForSimpleFixture(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "zola.toml"), []byte(`
base_url = "https://example.com"
title = "Fixture"
output_dir = "public"
generate_sitemap = true
generate_feeds = false
build_search_index = false
generate_robots_txt = true
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\n+++\nHello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "post.md"), []byte("+++\ntitle='Post'\n+++\nHello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "page.html"), []byte("<html><body>{{ page.content|safe }}</body></html>"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "section.html"), []byte("<html><body>{{ section.title }}</body></html>"), 0o644))

	outA := filepath.Join(root, "public-a")
	outB := filepath.Join(root, "public-b")
	require.NoError(t, harness.BuildWithKopkop(root, filepath.Join(root, "zola.toml"), outA, false))
	require.NoError(t, harness.BuildWithKopkop(root, filepath.Join(root, "zola.toml"), outB, false))

	diffs, err := harness.CompareDirectories(outA, outB, nil)
	require.NoError(t, err)
	require.Empty(t, diffs)
}

func TestBuild_RealZolaFixtures_Optional(t *testing.T) {
	t.Parallel()

	if os.Getenv("RUN_VENDORED_FIXTURES") != "1" {
		t.Skip("set RUN_VENDORED_FIXTURES=1 to run heavy vendored fixture builds")
	}

	for _, name := range []string{"test_site"} {
		name := name
		t.Run(name, func(t *testing.T) {
			root := filepath.Join("..", "fixtures", "zola", name)
			cfg := filepath.Join(root, "config.toml")
			if _, err := os.Stat(cfg); err != nil {
				cfg = filepath.Join(root, "zola.toml")
			}
			out := filepath.Join(t.TempDir(), "public")
			require.NoError(t, harness.BuildWithKopkop(root, cfg, out, false))
			require.FileExists(t, filepath.Join(out, "404.html"))
		})
	}
}
