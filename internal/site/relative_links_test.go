package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRelativeLinkStrategy(t *testing.T) {
	t.Parallel()

	files := func(strategy string) map[string]string {
		return map[string]string{
			"config.toml":                    "base_url = \"https://example.com/blog\"\ntitle = 'Site'\nlink_strategy = '" + strategy + "'\n",
			"content/_index.md":            "+++\ntitle = 'Home'\n+++\n",
			"content/a/index.md":           "+++\ntitle = 'A'\naliases = ['/old/']\n+++\n![pic](pic.png)\n\n[b](@/b.md#top)\n",
			"content/a/pic.png":            "x",
			"content/b.md":                 "+++\ntitle = 'B'\n+++\n",
			"templates/page.html":          "{{ page.content | safe }}",
			"templates/index.html":         "",
			"templates/taxonomy_list.html": "",
		}
	}

	for _, tc := range []struct {
		name     string
		strategy string
		want     []string
		notWant  []string
	}{
		{
			name:     "relative keeps links root-relative",
			strategy: "relative",
			want:     []string{`src="/blog/a/pic.png"`, `href="/blog/b/#top"`},
			notWant:  []string{"https://example.com"},
		},
		{
			name:     "absolute keeps full permalinks",
			strategy: "absolute",
			want:     []string{`src="https://example.com/blog/a/pic.png"`, `href="https://example.com/blog/b/#top"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for name, body := range files(tc.strategy) {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
			}

			s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "config.toml")})
			require.NoError(t, err)
			require.NoError(t, s.Load(false))
			require.NoError(t, s.Build(BuildOptions{BuildMode: BuildDisk}))

			page, err := os.ReadFile(filepath.Join(root, "public", "a", "index.html"))
			require.NoError(t, err)
			alias, err := os.ReadFile(filepath.Join(root, "public", "old", "index.html"))
			require.NoError(t, err)
			for _, w := range tc.want {
				require.Contains(t, string(page), w)
			}
			for _, n := range tc.notWant {
				require.NotContains(t, string(page), n)
				require.NotContains(t, string(alias), n)
			}
			if tc.strategy == "relative" {
				require.Contains(t, string(alias), `"/blog/a/"`)
			}
		})
	}
}
