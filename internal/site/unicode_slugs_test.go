package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnicodeSlugCollisionsBeforeRendering(t *testing.T) {
	for _, tc := range []struct {
		name, config, pageA, pageB, output string
	}{
		{"canonical Unicode page slugs", "", "+++\nslug='Café'\n+++\nBody", "+++\nslug='Cafe\u0301'\n+++\nBody", "café/index.html"},
		{"canonical Unicode taxonomy terms", "[[taxonomies]]\nname='tags'", "+++\n[taxonomies]\ntags=['Café']\n+++\nBody", "+++\n[taxonomies]\ntags=['Cafe\u0301']\n+++\nBody", "tags/café/index.html"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, body := range map[string]string{
				"config.toml":           "base_url='https://example.com'\n" + tc.config,
				"content/a.md":        tc.pageA,
				"content/b.md":        tc.pageB,
				"templates/page.html": "{{ missing() }}",
				"public/sentinel":     "keep",
			} {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
			}
			s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "config.toml")})
			require.NoError(t, err)
			for range 3 {
				err := s.Build(BuildOptions{})
				require.ErrorContains(t, err, "output collision")
				require.ErrorContains(t, err, tc.output)
			}
			body, err := os.ReadFile(filepath.Join(root, "public/sentinel"))
			require.NoError(t, err)
			require.Equal(t, "keep", string(body))
		})
	}
}
