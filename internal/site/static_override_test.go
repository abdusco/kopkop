package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStaticFilesReplaceGeneratedArtifacts(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		static string
	}{
		{"robots.txt", "robots.txt"},
		{"sitemap.xml", "sitemap.xml"},
		{"404.html", "404.html"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			files := map[string]string{
				"zola.toml":              "base_url = \"https://example.com\"\n",
				"content/_index.md":      "+++\ntitle='Home'\n+++\n",
				"templates/section.html": "<html></html>",
				"static/" + tc.static:    "from static",
			}
			for name, body := range files {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
			}

			s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "zola.toml")})
			require.NoError(t, err)
			require.NoError(t, s.Load(false))
			require.NoError(t, s.Build(BuildOptions{BuildMode: BuildDisk}))

			got, err := os.ReadFile(filepath.Join(root, "public", tc.static))
			require.NoError(t, err)
			require.Equal(t, "from static", string(got))
		})
	}
}
