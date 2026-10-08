package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestColocatedAssetsAreCopiedRecursively(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for name, body := range map[string]string{
		"config.toml":                         "base_url = \"https://example.com\"\nignored_content = [\"*.psd\"]\n",
		"content/post/index.md":             "+++\ntitle = 'Post'\n+++\n",
		"content/post/cover.png":            "cover",
		"content/post/images/deep/a.png":    "deep",
		"content/post/.DS_Store":            "junk",
		"content/post/images/.hidden":       "junk",
		"content/post/source.psd":           "ignored",
		"content/post/sub/_index.md":        "+++\ntitle = 'Sub'\n+++\n",
		"content/post/sub/not-an-asset.png": "belongs to the section",
		"content/post/bundle/index.md":      "+++\ntitle = 'Nested bundle'\n+++\n",
		"content/post/bundle/own.png":       "belongs to the nested bundle",
		"templates/page.html":               "{% for a in page.assets %}{{ a }};{% endfor %}",
		"templates/section.html":            "section",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
	}

	s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "config.toml")})
	require.NoError(t, err)
	require.NoError(t, s.Load(false))
	require.NoError(t, s.Build(BuildOptions{BuildMode: BuildDisk}))

	out := filepath.Join(root, "public")
	require.FileExists(t, filepath.Join(out, "post", "cover.png"))
	require.FileExists(t, filepath.Join(out, "post", "images", "deep", "a.png"))
	require.NoFileExists(t, filepath.Join(out, "post", ".DS_Store"))
	require.NoFileExists(t, filepath.Join(out, "post", "images", ".hidden"))
	require.NoFileExists(t, filepath.Join(out, "post", "source.psd"))
	require.NoFileExists(t, filepath.Join(out, "post", "sub", "not-an-asset.png"))
	// A nested bundle keeps its own assets.
	require.FileExists(t, filepath.Join(out, "post", "bundle", "own.png"))
}
