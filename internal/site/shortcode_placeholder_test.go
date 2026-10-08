package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/abdusco/kopkop/internal/markdown/shortcode"
)

func TestHTMLShortcodeSubstitution(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		page    string
		want    string
		wantErr string
	}{
		{
			name: "outputs land in call order",
			page: "{{ tag(v='a') }}\n\ntext\n\n{{ tag(v='b') }}\n",
			want: "[a]\n\n<p>text</p>\n[b]\n",
		},
		{
			name:    "literal placeholder in content is rejected",
			page:    "before " + shortcode.Placeholder + " after\n\n{{ tag(v='a') }}\n",
			wantErr: "reserved text",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for name, body := range map[string]string{
				"config.toml":                     "base_url = 'https://example.com'\ntitle = 'Site'\n",
				"content/_index.md":             "+++\ntitle = 'Home'\n+++\n",
				"content/post.md":               "+++\ntitle = 'Post'\n+++\n" + tc.page,
				"templates/shortcodes/tag.html": "[{{ v }}]",
				"templates/page.html":           "{{ page.content | safe }}",
				"templates/index.html":          "",
			} {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
			}

			s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "config.toml")})
			require.NoError(t, err)
			require.NoError(t, s.Load(false))
			err = s.Build(BuildOptions{BuildMode: BuildDisk})
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			got, err := os.ReadFile(filepath.Join(root, "public", "post", "index.html"))
			require.NoError(t, err)
			require.Contains(t, string(got), tc.want)
		})
	}
}
