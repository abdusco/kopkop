package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildBaseURLOverride(t *testing.T) {
	for _, preload := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "preloaded"}[preload], func(t *testing.T) {
			root := t.TempDir()
			for name, body := range map[string]string{
				"zola.toml":              "base_url='https://production.example'\ngenerate_feeds=true\n[[taxonomies]]\nname='tags'\nfeed=true\n",
				"content/_index.md":      "+++\ntitle='Home'\n+++\nHome",
				"content/post.md":        "+++\ntitle='Post'\ndate=2026-01-01\n[taxonomies]\ntags=['Go']\n+++\n[home](@/_index.md)",
				"templates/page.html":    "{{ page.permalink }} {{ page.content|safe }}",
				"templates/section.html": "{{ section.permalink }}",
			} {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
			}
			s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "zola.toml")})
			require.NoError(t, err)
			if preload {
				require.NoError(t, s.Load(false))
			}
			for _, origin := range []string{"https://preview.example/sub", "https://other.example"} {
				require.NoError(t, s.Build(BuildOptions{BuildMode: BuildMemory, BaseURL: origin}))
				require.Equal(t, origin+"/post/", s.Library.Permalinks["post.md"])
				for _, name := range []string{"post/index.html", "index.html", "tags/go/index.html", "atom.xml", "tags/go/atom.xml", "sitemap.xml"} {
					body, err := s.MemoryOutput.ReadFile(name)
					require.NoError(t, err, name)
					require.Contains(t, string(body), origin, name)
					require.NotContains(t, string(body), "production.example", name)
				}
				body, err := s.MemoryOutput.ReadFile("post/index.html")
				require.NoError(t, err)
				require.Contains(t, string(body), `href="`+origin+`/"`)
			}
		})
	}
}
