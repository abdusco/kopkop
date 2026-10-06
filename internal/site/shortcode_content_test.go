package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShortcodesInSectionsAndSummaries(t *testing.T) {
	for _, tc := range []struct {
		name      string
		raw       string
		wantError string
	}{
		{name: "before and after divider", raw: "# Heading\n\n[post](@/post.md) ![asset](image.png)\n\n{{ mark() }}\n\n{{ note() }}\n\n<!-- more -->\n\n{{ tail() }}"},
		{name: "without divider", raw: "{{ mark() }}\n\n{{ note() }}"},
		{name: "unknown shortcode", raw: "{{ missing() }}", wantError: "unknown shortcode"},
		{name: "shortcode template failure", raw: "{{ broken() }}", wantError: "missing.html"},
		{name: "broken markdown link", raw: "[missing](@/missing.md)", wantError: "broken relative link"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, body := range map[string]string{
				"zola.toml":                        "base_url='https://example.com'",
				"content/_index.md":                "+++\ntitle='Home'\n+++\n" + tc.raw,
				"content/post.md":                  "+++\ntitle='Post'\n+++\n" + tc.raw,
				"templates/page.html":              "{{ page.content|safe }} {{ page.summary|safe }}",
				"templates/section.html":           "{{ section.content|safe }}",
				"templates/shortcodes/mark.md":     "**Markdown shortcode**",
				"templates/shortcodes/note.html":   "<b>HTML shortcode</b>",
				"templates/shortcodes/tail.html":   "<b>After divider</b>",
				"templates/shortcodes/broken.html": `{% include "missing.html" %}`,
			} {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
			}
			s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "zola.toml")})
			require.NoError(t, err)
			err = s.Build(BuildOptions{BuildMode: BuildMemory})
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				// Exercise the section failure independently of the page failure.
				require.NoError(t, os.WriteFile(filepath.Join(root, "content/post.md"), []byte("+++\ntitle='Post'\n+++\nValid"), 0o644))
				s.Library = nil
				err = s.Build(BuildOptions{BuildMode: BuildMemory})
				require.ErrorContains(t, err, tc.wantError)
				require.ErrorContains(t, err, "_index.md")
				return
			}
			require.NoError(t, err)
			for _, body := range []string{s.Library.Pages["post.md"].Content, s.Library.Sections["_index.md"].Content} {
				require.Contains(t, body, "<strong>Markdown shortcode</strong>")
				require.Contains(t, body, "<b>HTML shortcode</b>")
			}
			if !strings.Contains(tc.raw, "<!-- more -->") {
				require.Nil(t, s.Library.Pages["post.md"].Summary)
				return
			}
			summary := s.Library.Pages["post.md"].Summary
			require.NotNil(t, summary)
			require.Contains(t, *summary, `<h1 id="heading">`)
			require.Contains(t, *summary, `href="https://example.com/post/"`)
			require.Contains(t, *summary, `src="https://example.com/post/image.png"`)
			require.Contains(t, *summary, "<b>HTML shortcode</b>")
			require.NotContains(t, *summary, "After divider")
			require.True(t, strings.HasPrefix(s.Library.Pages["post.md"].Content, *summary))
		})
	}
}
