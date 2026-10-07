package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShortcodesAndErrorPagesHaveConfigAndHelpers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for name, body := range map[string]string{
		"zola.toml":                      "base_url = \"https://example.com/blog\"\ntitle = 'Site'\n",
		"content/_index.md":              "+++\ntitle = 'Home'\n+++\n{{ info() }}",
		"content/about.md":               "+++\ntitle = 'About'\ndate = 2024-05-06\n+++\n{{ info() }}",
		"templates/shortcodes/info.html": "[{{ config.title }}|{{ get_url(path='a.css') }}|{{ get_page(path='about.md').title }}|{{ get_section(path='_index.md').title }}]",
		"templates/page.html":            "{{ page.content | safe }}",
		"templates/index.html":           "{{ section.content | safe }}",
		"templates/404.html":             "{{ get_page(path='about.md').title }}",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
	}

	s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "zola.toml")})
	require.NoError(t, err)
	require.NoError(t, s.Load(false))
	require.NoError(t, s.Build(BuildOptions{BuildMode: BuildDisk}))

	want := "[Site|https://example.com/blog/a.css|About|Home]"
	for _, name := range []string{"index.html", "about/index.html"} {
		got, err := os.ReadFile(filepath.Join(root, "public", name))
		require.NoError(t, err)
		require.Contains(t, string(got), want, name)
	}
	notFound, err := os.ReadFile(filepath.Join(root, "public", "404.html"))
	require.NoError(t, err)
	require.Equal(t, "About\n", string(notFound))
}
