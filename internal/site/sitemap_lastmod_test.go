package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSitemapLastmod(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		page string
		want string
	}{
		{name: "toml local date", page: "+++\ntitle='A'\ndate=2024-03-04\n+++\n", want: "2024-03-04"},
		{name: "toml offset datetime", page: "+++\ntitle='A'\ndate=2024-03-04T10:20:30Z\n+++\n", want: "2024-03-04T10:20:30Z"},
		{name: "toml datetime with offset", page: "+++\ntitle='A'\ndate=2024-03-04T10:20:30+02:00\n+++\n", want: "2024-03-04T08:20:30Z"},
		{name: "toml date string", page: "+++\ntitle='A'\ndate='2024-03-04'\n+++\n", want: "2024-03-04"},
		{name: "toml datetime string", page: "+++\ntitle='A'\ndate='2024-03-04T10:20:30Z'\n+++\n", want: "2024-03-04T10:20:30Z"},
		{name: "yaml date", page: "---\ntitle: A\ndate: 2024-03-04\n---\n", want: "2024-03-04"},
		{name: "yaml datetime", page: "---\ntitle: A\ndate: 2024-03-04T10:20:30Z\n---\n", want: "2024-03-04T10:20:30Z"},
		{name: "crlf front matter", page: "+++\r\ntitle='A'\r\ndate=2024-03-04T10:20:30Z\r\n+++\r\n", want: "2024-03-04T10:20:30Z"},
		{name: "updated wins over date", page: "+++\ntitle='A'\ndate=2024-03-04\nupdated=2025-01-02T03:04:05Z\n+++\n", want: "2025-01-02T03:04:05Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for name, body := range map[string]string{
				"zola.toml":             "base_url='https://example.com'\ngenerate_sitemap=true\n",
				"content/_index.md":     "+++\ntitle='Home'\n+++\n",
				"content/a.md":          tc.page,
				"templates/page.html":   "",
				"templates/index.html":  "",
				"templates/sitemap.xml": "{% for e in entries %}{{ e.permalink }}|{{ e.updated | default(value='') }};{% endfor %}",
			} {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
			}

			s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "zola.toml")})
			require.NoError(t, err)
			require.NoError(t, s.Load(false))
			require.NoError(t, s.Build(BuildOptions{BuildMode: BuildDisk}))

			out, err := os.ReadFile(filepath.Join(root, "public", "sitemap.xml"))
			require.NoError(t, err)
			require.Contains(t, string(out), "https://example.com/a/|"+tc.want+";")
		})
	}
}
