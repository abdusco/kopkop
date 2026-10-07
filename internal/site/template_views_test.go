package site

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedTemplateViewsRefreshBetweenBuilds(t *testing.T) {
	for _, concurrency := range []int{1, 4} {
		t.Run(fmt.Sprintf("concurrency=%d", concurrency), func(t *testing.T) {
			root := t.TempDir()
			for name, body := range map[string]string{
				"zola.toml":              "base_url='https://example.com'\ntitle='Initial'\n[[taxonomies]]\nname='tags'\n",
				"content/blog/_index.md": "+++\ntitle='Blog'\npaginate_by=2\nsort_by='weight'\n+++\n**Initial section**",
				"content/blog/a.md":      "+++\ntitle='A'\nweight=1\n[taxonomies]\ntags=['Go']\n+++\n**Initial page**",
				"content/blog/b.md":      "+++\ntitle='B'\nweight=2\n+++\nB",
				"content/blog/c.md":      "+++\ntitle='C'\nweight=3\n+++\nC",
				"templates/page.html":    `{{ page.title }}|{{ current_url }}|{{ config.title }}|{{ get_page(path="blog/a.md").content|safe }}|{{ section.content|safe }}|{{ section.pages|length }}|{{ get_taxonomy_term(kind="tags", term="Go").pages[0].title }}`,
				"templates/section.html": `{{ section.pages|length }}|{{ get_section(path="blog/_index.md").pages|length }}|{{ get_page(path="blog/a.md").title }}`,
			} {
				p := filepath.Join(root, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
				require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
			}
			s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "zola.toml")})
			require.NoError(t, err)
			for _, step := range []struct{ configTitle, pageTitle, body, sectionBody string }{
				{"Initial", "A", "Initial page", "Initial section"},
				{"Updated", "Updated A", "Updated page", "Updated section"},
			} {
				if s.Library != nil {
					s.Config.Title = step.configTitle
					s.Library.Pages["blog/a.md"].Meta.Title = step.pageTitle
					s.Library.Pages["blog/a.md"].RawContent = "**" + step.body + "**"
					s.Library.Sections["blog/_index.md"].RawContent = "**" + step.sectionBody + "**"
				}
				require.NoError(t, s.Build(BuildOptions{BuildMode: BuildMemory, Concurrency: concurrency}))
				for _, page := range []struct{ slug, title string }{{"a", step.pageTitle}, {"b", "B"}, {"c", "C"}} {
					data, err := s.MemoryOutput.ReadFile("blog/" + page.slug + "/index.html")
					require.NoError(t, err)
					require.Contains(t, string(data), page.title+"|https://example.com/blog/"+page.slug+"/|"+step.configTitle)
					require.Contains(t, string(data), "<strong>"+step.body+"</strong>")
					require.Contains(t, string(data), "<strong>"+step.sectionBody+"</strong>")
					require.Contains(t, string(data), "|3|"+step.pageTitle)
				}
				for _, pager := range []struct{ output, want string }{{"blog/index.html", "2|3|"}, {"blog/page/2/index.html", "1|3|"}} {
					data, err := s.MemoryOutput.ReadFile(pager.output)
					require.NoError(t, err)
					require.Equal(t, pager.want+step.pageTitle+"\n", string(data))
				}
			}
			// Loading new source content and overriding the origin must discard
			// the previous views as well as the previous derived permalinks.
			require.NoError(t, os.WriteFile(filepath.Join(root, "content/blog/a.md"), []byte("+++\ntitle='Reloaded A'\nweight=1\n[taxonomies]\ntags=['Go']\n+++\n**Reloaded page**"), 0o644))
			require.NoError(t, s.Load(false))
			require.NoError(t, s.Build(BuildOptions{BuildMode: BuildMemory, BaseURL: "https://preview.example", Concurrency: concurrency}))
			data, err := s.MemoryOutput.ReadFile("blog/a/index.html")
			require.NoError(t, err)
			require.Contains(t, string(data), "Reloaded A|https://preview.example/blog/a/")
			require.Contains(t, string(data), "<strong>Reloaded page</strong>")
		})
	}
}
