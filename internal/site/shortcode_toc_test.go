package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHTMLShortcodeSeesPageTOC(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for name, body := range map[string]string{
		"config.toml":                     "base_url = \"https://example.com\"\ntitle = 'Site'\n",
		"content/_index.md":             "+++\ntitle = 'Home'\n+++\n",
		"content/post.md":               "+++\ntitle = 'Post'\n+++\n{{ toc() }}\n\n## One\n\n### Two\n",
		"templates/shortcodes/toc.html": "{% for h in page.toc %}<{{ h.level }}:{{ h.id }}:{{ h.title }}>{% endfor %}",
		"templates/page.html":           "{{ page.content | safe }}",
		"templates/index.html":          "",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
	}

	s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "config.toml")})
	require.NoError(t, err)
	require.NoError(t, s.Load(false))
	require.NoError(t, s.Build(BuildOptions{BuildMode: BuildDisk}))

	got, err := os.ReadFile(filepath.Join(root, "public", "post", "index.html"))
	require.NoError(t, err)
	require.Contains(t, string(got), "<2:one:One><3:two:Two>")
}
