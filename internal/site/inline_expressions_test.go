package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarkdownInlineExpressions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		raw       string
		want      []string
		wantError string
	}{
		{name: "URL in link", raw: `[Guide]({{ get_url(path='guide.pdf') }})`, want: []string{`<a href="https://example.com/blog/guide.pdf">Guide</a>`}},
		{name: "functions filters and context", raw: `{{ config.title | upper }} {{ get_page(path='post.md').title }} {{ get_section(path='_index.md').title }}`, want: []string{"SITE Post Home"}},
		{name: "generated markdown and heading", raw: `# {{ '**Title**' }}`, want: []string{`<h1 id="title"><strong>Title</strong></h1>`}},
		{name: "relative data", raw: `{{ load_data('./data.json').label }}`, want: []string{"Local data"}},
		{name: "quoted closing delimiter", raw: `{{ 'a }} b' }}`, want: []string{"a }} b"}},
		{name: "dictionary expression", raw: `{{{'a': 'value'}['a']}}`, want: []string{"value"}},
		{name: "code span", raw: "`{{ missing() }}` {{ 1 + 2 }}", want: []string{"<code>{{ missing() }}</code> 3"}},
		{name: "multiple backticks", raw: "`` ` {{ missing() }} ` ``", want: []string{"<code>` {{ missing() }} `</code>"}},
		{name: "multiline code span", raw: "`{{ missing() }}\n{{ missing_again() }}`", want: []string{"<code>{{ missing() }} {{ missing_again() }}</code>"}},
		{name: "fenced code", raw: "```jinja\n{{ missing() }}\n```\n\n{{ 3 }}", want: []string{"{{ missing() }}", "<p>3</p>"}},
		{name: "tilde fence in quote", raw: "> ~~~~\n> {{ missing() }}\n> ~~~~", want: []string{"<blockquote>", "{{ missing() }}"}},
		{name: "unclosed fence", raw: "```\n{{ missing() }}", want: []string{"{{ missing() }}"}},
		{name: "indented code", raw: "    {{ missing() }}\n\n{{ 3 }}", want: []string{"<pre><code>{{ missing() }}", "<p>3</p>"}},
		{name: "escaped expression", raw: `\{{ missing() }} {{ 3 }}`, want: []string{"{{ missing() }} 3"}},
		{name: "ignored expression", raw: `{{/* missing() */}} {{ 3 }}`, want: []string{"{{ missing() }} 3"}},
		{name: "output evaluated once", raw: `{{ '{{ missing() }}' }}`, want: []string{"{{ missing() }}"}},
		{name: "shortcodes alongside expressions", raw: "{{ mark() }}\n\n{{ note() }}\n\n{{ 3 }}", want: []string{"<strong>Markdown shortcode</strong>", "<b>HTML shortcode</b>", "<p>3</p>"}},
		{name: "shortcode literal in code", raw: "`{{ note() }}`\n\n```\n{% wrap() %} {{ mark() }} {% end %}\n```", want: []string{"<code>{{ note() }}</code>", "{% wrap() %} {{ mark() }} {% end %}"}},
		{name: "body shortcode with code and expression", raw: "{% wrap() %}\n\n`{% end %} {{ missing() }}`\n\n{{ 3 }}\n{% end %}", want: []string{"<code>{% end %} {{ missing() }}</code>", "<p>3</p>"}},
		{name: "quoted body tag in expression", raw: `{% wrap() %}{{ '{% end %}' }} {{ 3 }}{% end %}`, want: []string{"{% end %} 3"}},
		{name: "summary", raw: "{{ '**Before**' }}\n\n<!-- more -->\n\n{{ '**After**' }}", want: []string{"<strong>Before</strong>", "<strong>After</strong>"}},
		{name: "page and section context", raw: `{{ page.title if page is defined else section.title }}`, want: []string{"<p>"}},
		{name: "unknown function", raw: `{{ missing() }}`, wantError: "unknown function"},
		{name: "invalid expression", raw: `{{ 1 + }}`, wantError: "inline expression"},
		{name: "unclosed expression", raw: `{{ get_url(path='guide.pdf')`, wantError: "unterminated inline expression"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for name, body := range map[string]string{
				"config.toml":                    "base_url='https://example.com/blog'\ntitle='Site'",
				"content/_index.md":              "+++\ntitle='Home'\n+++\n" + tc.raw,
				"content/post.md":                "+++\ntitle='Post'\n+++\n" + tc.raw,
				"content/data.json":              `{"label":"Local data"}`,
				"templates/page.html":            "{{ page.content|safe }}",
				"templates/section.html":         "{{ section.content|safe }}",
				"templates/shortcodes/mark.md":   "**Markdown shortcode**",
				"templates/shortcodes/note.html": "<b>HTML shortcode</b>",
				"templates/shortcodes/wrap.md":   "{{ body }}",
			} {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
			}
			s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "config.toml")})
			require.NoError(t, err)
			err = s.Build(BuildOptions{BuildMode: BuildMemory})
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				require.ErrorContains(t, err, "post.md")
				return
			}
			require.NoError(t, err)
			for _, body := range []string{s.Library.Pages["post.md"].Content, s.Library.Sections["_index.md"].Content} {
				for _, want := range tc.want {
					require.Contains(t, body, want)
				}
			}
			if tc.name == "page and section context" {
				require.Contains(t, s.Library.Pages["post.md"].Content, "Post")
				require.Contains(t, s.Library.Sections["_index.md"].Content, "Home")
			}
			if tc.name == "generated markdown and heading" {
				require.Len(t, s.Library.Pages["post.md"].TOC, 1)
				require.Equal(t, "Title", s.Library.Pages["post.md"].TOC[0].Title)
			}
			if tc.name == "summary" {
				summary := s.Library.Pages["post.md"].Summary
				require.NotNil(t, summary)
				require.Contains(t, *summary, "<strong>Before</strong>")
				require.NotContains(t, *summary, "After")
			}
		})
	}
}
