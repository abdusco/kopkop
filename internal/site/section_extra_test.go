package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSectionExtraIsAvailableInTemplates(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for name, body := range map[string]string{
		"config.toml":              "base_url = \"https://example.com\"\n",
		"content/_index.md":      "+++\n[extra]\ncolor = 'red'\n+++\n",
		"content/blog/_index.md": "+++\ntitle = 'Blog'\n+++\n",
		"templates/section.html": "{{ section.title }}:{{ section.extra.color | default('none') }}",
		"templates/index.html":   "home:{{ section.extra.color }}",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
	}

	s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "config.toml")})
	require.NoError(t, err)
	require.NoError(t, s.Load(false))
	require.NoError(t, s.Build(BuildOptions{BuildMode: BuildDisk}))

	home, err := os.ReadFile(filepath.Join(root, "public", "index.html"))
	require.NoError(t, err)
	require.Equal(t, "home:red\n", string(home))
	blog, err := os.ReadFile(filepath.Join(root, "public", "blog", "index.html"))
	require.NoError(t, err)
	require.Equal(t, "Blog:none\n", string(blog))
}
