package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPageNeighbors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, sortBy, page, want string
	}{
		{"date middle", "date", "b", "earlier=A later=C lighter= heavier="},
		{"date newest", "date", "c", "earlier=B later= lighter= heavier="},
		{"weight middle", "weight", "b", "earlier= later= lighter=A heavier=C"},
		{"unsorted", "none", "b", "earlier= later= lighter= heavier="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			files := map[string]string{
				"zola.toml":              "base_url = \"https://example.com\"\n",
				"content/blog/_index.md": "+++\nsort_by = '" + tc.sortBy + "'\n+++\n",
				"content/blog/a.md":      "+++\ntitle = 'A'\ndate = 2024-01-01\nweight = 1\n+++\n",
				"content/blog/b.md":      "+++\ntitle = 'B'\ndate = 2024-02-01\nweight = 2\n+++\n",
				"content/blog/c.md":      "+++\ntitle = 'C'\ndate = 2024-03-01\nweight = 3\n+++\n",
				"templates/section.html": "x",
				"templates/page.html":    "earlier={% if page.earlier %}{{ page.earlier.title }}{% endif %} later={% if page.later %}{{ page.later.title }}{% endif %} lighter={% if page.lighter %}{{ page.lighter.title }}{% endif %} heavier={% if page.heavier %}{{ page.heavier.title }}{% endif %}",
			}
			for name, body := range files {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
			}
			s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "zola.toml")})
			require.NoError(t, err)
			require.NoError(t, s.Load(false))
			require.NoError(t, s.Build(BuildOptions{BuildMode: BuildDisk}))

			got, err := os.ReadFile(filepath.Join(root, "public", "blog", tc.page, "index.html"))
			require.NoError(t, err)
			require.Equal(t, tc.want+"\n", string(got))
		})
	}
}
