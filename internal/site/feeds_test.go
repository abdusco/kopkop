package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func buildFeedSite(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
	}
	s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "zola.toml")})
	require.NoError(t, err)
	require.NoError(t, s.Load(false))
	require.NoError(t, s.Build(BuildOptions{BuildMode: BuildDisk}))
	return filepath.Join(root, "public")
}

func TestFeedsAndSitemapUseSiteTemplates(t *testing.T) {
	t.Parallel()

	out := buildFeedSite(t, map[string]string{
		"zola.toml":              "base_url = \"https://example.com\"\ntitle = 'Site'\ngenerate_feeds = true\nfeed_filenames = ['atom.xml']\n",
		"content/_index.md":      "+++\n+++\n",
		"content/a.md":           "+++\ntitle = 'A'\ndate = 2024-01-01\n+++\nbody",
		"content/b.md":           "+++\ntitle = 'B'\ndate = 2024-02-01\n+++\nbody",
		"templates/section.html": "x",
		"templates/page.html":    "x",
		"templates/atom.xml":     "CUSTOM {{ feed_url }} {{ last_updated }} {% for p in pages %}[{{ p.title }}]{% endfor %}",
		"templates/sitemap.xml":  "SITEMAP {% for e in entries %}{{ e.permalink }};{% endfor %}",
	})

	feed, err := os.ReadFile(filepath.Join(out, "atom.xml"))
	require.NoError(t, err)
	require.Equal(t, "CUSTOM https://example.com/atom.xml 2024-02-01T00:00:00+00:00 [B][A]\n", string(feed))

	sitemap, err := os.ReadFile(filepath.Join(out, "sitemap.xml"))
	require.NoError(t, err)
	require.Contains(t, string(sitemap), "SITEMAP ")
	require.Contains(t, string(sitemap), "https://example.com/a/;")
}

func TestRSSFeedFilenameProducesRSSAndHonoursFeedLimit(t *testing.T) {
	t.Parallel()

	out := buildFeedSite(t, map[string]string{
		"zola.toml":              "base_url = \"https://example.com\"\ntitle = 'Site'\ngenerate_feeds = true\nfeed_filenames = ['rss.xml']\nfeed_limit = 1\n",
		"content/_index.md":      "+++\n+++\n",
		"content/a.md":           "+++\ntitle = 'Older'\ndate = 2024-01-01\n+++\nbody",
		"content/b.md":           "+++\ntitle = 'Newer'\ndate = 2024-02-01\n+++\nbody",
		"templates/section.html": "x",
		"templates/page.html":    "x",
	})

	feed, err := os.ReadFile(filepath.Join(out, "rss.xml"))
	require.NoError(t, err)
	require.Contains(t, string(feed), `<rss version="2.0"`)
	require.Contains(t, string(feed), "<title>Newer</title>")
	require.NotContains(t, string(feed), "Older")
	require.Contains(t, string(feed), "<pubDate>Thu, 01 Feb 2024 00:00:00 +0000</pubDate>")
}
