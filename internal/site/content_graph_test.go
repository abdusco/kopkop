package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTemplatesSeeCompleteRenderedGraph(t *testing.T) {
	for _, mode := range []BuildMode{BuildDisk, BuildMemory, BuildBoth} {
		t.Run(map[BuildMode]string{BuildDisk: "disk", BuildMemory: "memory", BuildBoth: "both"}[mode], func(t *testing.T) {
			root := t.TempDir()
			for name, body := range map[string]string{
				"config.toml":              "base_url='https://example.com'",
				"content/a/_index.md":    "+++\ntitle='A'\n+++\n**Section A**",
				"content/z/_index.md":    "+++\ntitle='Z'\nrender=false\n+++\n**Section Z**",
				"content/post.md":        "+++\ntitle='Post'\n+++\n**Page Post**",
				"content/hidden.md":      "+++\ntitle='Hidden'\nrender=false\n+++\n**Page Hidden**",
				"templates/page.html":    `{% set a=get_section(path="a/_index.md") %}{% set z=get_section(path="z/_index.md") %}{{ a.content|safe }}{{ z.content|safe }}`,
				"templates/section.html": `{% set z=get_section(path="z/_index.md") %}{% set p=get_page(path="post.md") %}{% set h=get_page(path="hidden.md") %}{{ z.content|safe }}{{ p.content|safe }}{{ h.content|safe }}`,
			} {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
			}
			s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "config.toml")})
			require.NoError(t, err)
			for i := 0; i < 2; i++ {
				require.NoError(t, s.Build(BuildOptions{BuildMode: mode}))
				for _, tc := range []struct {
					name string
					want []string
				}{
					{"post/index.html", []string{"<strong>Section A</strong>", "<strong>Section Z</strong>"}},
					{"a/index.html", []string{"<strong>Section Z</strong>", "<strong>Page Post</strong>", "<strong>Page Hidden</strong>"}},
				} {
					output := s.OutputFS
					if mode != BuildDisk {
						output = s.MemoryOutput
					}
					body, err := output.ReadFile(tc.name)
					require.NoError(t, err)
					for _, want := range tc.want {
						require.Contains(t, string(body), want)
					}
				}
			}
		})
	}
}
