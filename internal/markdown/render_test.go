package markdown

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	ghtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

func parseDoc(t *testing.T, in string) (ast.Node, []byte) {
	t.Helper()
	md := goldmark.New()
	source := []byte(in)
	doc := md.Parser().Parse(text.NewReader(source))
	return doc, source
}

func renderDocHTML(t *testing.T, doc ast.Node, source []byte) (string, error) {
	t.Helper()
	md := goldmark.New(
		goldmark.WithRendererOptions(
			ghtml.WithUnsafe(),
			ghtml.WithXHTML(),
		),
	)
	buf := bytes.NewBuffer(nil)
	err := md.Renderer().Render(buf, source, doc)
	return buf.String(), err
}

func TestRenderContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		markdown     string
		ctx          RenderContext
		assertResult func(t *testing.T, html string, err error, res Rendered)
	}{
		{
			name:     "summary divider",
			markdown: "hello\n\n<!-- more -->\n\nworld",
			ctx:      RenderContext{},
			assertResult: func(t *testing.T, html string, err error, res Rendered) {
				assert.NoError(t, err)
				assert.Contains(t, html, `<span id="continue-reading"></span>`)
				require.NotNil(t, res.Summary)
				assert.Contains(t, *res.Summary, "<p>hello</p>")
			},
		},
		{
			name:     "duplicate heading ids and toc",
			markdown: "# Example\n\n# Example\n",
			ctx:      RenderContext{},
			assertResult: func(t *testing.T, html string, err error, res Rendered) {
				assert.NoError(t, err)
				assert.Contains(t, html, `id="example"`)
				assert.Contains(t, html, `id="example-1"`)
				require.Len(t, res.TOC, 2)
				assert.Equal(t, "example", res.TOC[0].ID)
				assert.Equal(t, "example-1", res.TOC[1].ID)
			},
		},
		{
			name:     "heading inline code in slug and toc",
			markdown: "# text here `with code`",
			ctx:      RenderContext{},
			assertResult: func(t *testing.T, html string, err error, res Rendered) {
				assert.NoError(t, err)
				assert.Contains(t, html, `id="text-here-with-code"`)
				require.Len(t, res.TOC, 1)
				assert.Equal(t, "text here with code", res.TOC[0].Title)
				assert.Equal(t, "text-here-with-code", res.TOC[0].ID)
			},
		},
		{
			name:     "internal external and colocated links",
			markdown: "[internal](@/content/posts/hello.md#intro) [ext](https://example.org) [asset](image.png)",
			ctx: RenderContext{
				Permalinks:           map[string]string{"content/posts/hello.md": "https://example.com/posts/hello/"},
				CurrentPagePermalink: "https://example.com/posts/current/",
			},
			assertResult: func(t *testing.T, html string, err error, res Rendered) {
				assert.NoError(t, err)
				assert.Contains(t, html, `href="https://example.com/posts/hello/#intro"`)
				assert.Contains(t, html, `href="https://example.com/posts/current/image.png"`)
				assert.Equal(t, []string{"https://example.org"}, res.ExternalLinks)
			},
		},
		{
			name:     "broken internal link error",
			markdown: "[broken](@/content/posts/missing.md)",
			ctx:      RenderContext{Permalinks: map[string]string{}},
			assertResult: func(t *testing.T, html string, err error, _ Rendered) {
				assert.Error(t, err)
				assert.ErrorContains(t, err, "broken relative link")
				assert.Equal(t, "", html)
			},
		},
		{
			name:     "anchor links toggle on",
			markdown: "# Heading",
			ctx:      RenderContext{InsertAnchorLinks: true},
			assertResult: func(t *testing.T, html string, err error, _ Rendered) {
				assert.NoError(t, err)
				assert.Contains(t, html, `class="kopkop-anchor"`)
				assert.Contains(t, html, `href="#heading"`)
			},
		},
		{
			name:     "anchor links toggle off",
			markdown: "# Heading",
			ctx:      RenderContext{InsertAnchorLinks: false},
			assertResult: func(t *testing.T, html string, err error, _ Rendered) {
				assert.NoError(t, err)
				assert.NotContains(t, html, `class="kopkop-anchor"`)
			},
		},
		{
			name:     "continue reading not wrapped paragraph",
			markdown: "Before\n\n<!-- more -->\n\nAfter",
			ctx:      RenderContext{},
			assertResult: func(t *testing.T, html string, err error, _ Rendered) {
				assert.NoError(t, err)
				assert.Contains(t, html, `<span id="continue-reading"></span>`)
				assert.NotContains(t, html, `<p><span id="continue-reading"></span></p>`)
			},
		},
		{
			name:     "fenced code language attrs",
			markdown: "```rust\nfn main() {}\n```",
			ctx:      RenderContext{},
			assertResult: func(t *testing.T, html string, err error, _ Rendered) {
				assert.NoError(t, err)
				assert.Contains(t, html, `<pre data-lang="rust" class="language-rust "><code class="language-rust" data-lang="rust">`)
			},
		},
		{
			name:     "highlighted code known theme",
			markdown: "```go\npackage main\n```",
			ctx:      RenderContext{HighlightTheme: "github"},
			assertResult: func(t *testing.T, html string, err error, _ Rendered) {
				assert.NoError(t, err)
				assert.Contains(t, html, `data-highlighted="true"`)
				assert.Contains(t, html, `class="language-go z-code z-chroma"`)
				assert.Contains(t, html, `<span class="z-`)
			},
		},
		{
			name:     "highlighted code unknown theme fallback",
			markdown: "```go\npackage main\n```",
			ctx:      RenderContext{HighlightTheme: "missing-theme"},
			assertResult: func(t *testing.T, html string, err error, _ Rendered) {
				assert.NoError(t, err)
				assert.Contains(t, html, `data-highlighted="true"`)
				assert.Contains(t, html, `class="language-go z-code z-chroma"`)
				assert.Contains(t, html, `<span class="z-`)
			},
		},
		{
			name:     "external links target blank",
			markdown: "[ext](https://example.org)",
			ctx:      RenderContext{ExternalLinksTargetBlank: true},
			assertResult: func(t *testing.T, html string, err error, _ Rendered) {
				assert.NoError(t, err)
				assert.Contains(t, html, `href="https://example.org"`)
				assert.Contains(t, html, `target="_blank"`)
				assert.Contains(t, html, `rel="noopener"`)
			},
		},
		{
			name: "gfm table strikethrough and task list",
			markdown: `| a | b |
| - | - |
| 1 | 2 |

~~gone~~

- [x] done`,
			ctx: RenderContext{},
			assertResult: func(t *testing.T, html string, err error, _ Rendered) {
				assert.NoError(t, err)
				assert.Contains(t, html, `<table>`)
				assert.Contains(t, html, `<del>gone</del>`)
				assert.Contains(t, html, `type="checkbox"`)
			},
		},
		{
			name: "footnotes",
			markdown: `Footnote ref[^1]

[^1]: Footnote text`,
			ctx: RenderContext{},
			assertResult: func(t *testing.T, html string, err error, _ Rendered) {
				assert.NoError(t, err)
				assert.Contains(t, html, `fnref:1`)
				assert.Contains(t, html, `id="fn:1"`)
				assert.Contains(t, html, `Footnote text`)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := RenderContent(tc.markdown, tc.ctx)
			html := ""
			if err == nil {
				html = res.Body
			}
			tc.assertResult(t, html, err, res)
		})
	}
}

func TestHighlightCSS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		theme        string
		assertResult func(t *testing.T, css string, err error)
	}{
		{
			name:  "known theme",
			theme: "github",
			assertResult: func(t *testing.T, css string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, css, ".z-")
			},
		},
		{
			name:  "unknown theme fallback",
			theme: "does-not-exist",
			assertResult: func(t *testing.T, css string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, css, ".z-")
			},
		},
		{
			name:  "empty theme errors",
			theme: "",
			assertResult: func(t *testing.T, css string, err error) {
				assert.Error(t, err)
				assert.ErrorContains(t, err, "must not be empty")
				assert.Equal(t, "", css)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			css, err := HighlightCSS(tc.theme)
			tc.assertResult(t, css, err)
		})
	}
}

func TestHeadingIDsAreUnique(t *testing.T) {
	t.Parallel()

	res, err := RenderContent("## Foo\n\n## Foo\n\n## Foo 1\n\n### Foo\n\n## Foo 1\n", RenderContext{})
	require.NoError(t, err)
	seen := map[string]bool{}
	for _, h := range res.TOC {
		require.False(t, seen[h.ID], "duplicate id %q", h.ID)
		seen[h.ID] = true
	}
	require.Len(t, res.TOC, 5)
	require.Equal(t, "foo", res.TOC[0].ID)
	require.Equal(t, "foo-1", res.TOC[1].ID)
}

func TestTransformHeadings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		markdown      string
		insertAnchors bool
		assertResult  func(t *testing.T, html string, err error)
	}{
		{
			name:          "ids only",
			markdown:      "# One\n\n## Two",
			insertAnchors: false,
			assertResult: func(t *testing.T, html string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, html, `<h1 id="one">One</h1>`)
				assert.Contains(t, html, `<h2 id="two">Two</h2>`)
				assert.NotContains(t, html, `class="kopkop-anchor"`)
			},
		},
		{
			name:          "duplicate headings with anchors",
			markdown:      "# Example\n\n# Example",
			insertAnchors: true,
			assertResult: func(t *testing.T, html string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, html, `id="example"`)
				assert.Contains(t, html, `id="example-1"`)
				assert.Equal(t, 2, strings.Count(html, `class="kopkop-anchor"`))
				assert.Contains(t, html, `href="#example"`)
				assert.Contains(t, html, `href="#example-1"`)
			},
		},
		{
			name:          "inline code participates in slug",
			markdown:      "# text here `with code`",
			insertAnchors: true,
			assertResult: func(t *testing.T, html string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, html, `id="text-here-with-code"`)
				assert.Contains(t, html, `href="#text-here-with-code"`)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, source := parseDoc(t, tc.markdown)
			transformHeadings(doc, source, tc.insertAnchors)
			html, err := renderDocHTML(t, doc, source)
			tc.assertResult(t, html, err)
		})
	}
}

func TestTransformInternalLinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		markdown     string
		permalinks   map[string]string
		assertResult func(t *testing.T, html string, err error)
	}{
		{
			name:       "rewrites internal link and image",
			markdown:   "[internal](@/content/posts/hello.md#intro) ![img](@/content/posts/hello.md)",
			permalinks: map[string]string{"content/posts/hello.md": "https://example.com/posts/hello/"},
			assertResult: func(t *testing.T, html string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, html, `href="https://example.com/posts/hello/#intro"`)
				assert.Contains(t, html, `src="https://example.com/posts/hello/"`)
			},
		},
		{
			name:       "non internal destinations unchanged",
			markdown:   "[ext](https://example.org) ![img](photo.png)",
			permalinks: map[string]string{},
			assertResult: func(t *testing.T, html string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, html, `href="https://example.org"`)
				assert.Contains(t, html, `src="photo.png"`)
			},
		},
		{
			name:       "broken internal link returns error",
			markdown:   "[broken](@/content/posts/missing.md)",
			permalinks: map[string]string{},
			assertResult: func(t *testing.T, html string, err error) {
				assert.Error(t, err)
				assert.ErrorContains(t, err, "broken relative link")
				assert.Equal(t, "", html)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, source := parseDoc(t, tc.markdown)
			err := transformInternalLinks(doc, RenderContext{Permalinks: tc.permalinks})
			html := ""
			if err == nil {
				html, err = renderDocHTML(t, doc, source)
			}
			tc.assertResult(t, html, err)
		})
	}
}

func TestTransformColocatedAssetLinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		markdown     string
		baseURL      string
		assertResult func(t *testing.T, html string, err error)
	}{
		{
			name:     "rewrites relative assets",
			markdown: "[asset](image.png) ![img](media/photo.jpg)",
			baseURL:  "https://example.com/posts/current/",
			assertResult: func(t *testing.T, html string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, html, `href="https://example.com/posts/current/image.png"`)
				assert.Contains(t, html, `src="https://example.com/posts/current/media/photo.jpg"`)
			},
		},
		{
			name:     "skips non-colocated paths",
			markdown: "[root](/asset.png) ![img](../photo.jpg)",
			baseURL:  "https://example.com/posts/current/",
			assertResult: func(t *testing.T, html string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, html, `href="/asset.png"`)
				assert.Contains(t, html, `src="../photo.jpg"`)
			},
		},
		{
			name:     "skips schema links",
			markdown: "[mail](mailto:test@example.com) ![img](https://cdn.example.com/a.png)",
			baseURL:  "https://example.com/posts/current/",
			assertResult: func(t *testing.T, html string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, html, `href="mailto:test@example.com"`)
				assert.Contains(t, html, `src="https://cdn.example.com/a.png"`)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, source := parseDoc(t, tc.markdown)
			transformColocatedAssetLinks(doc, RenderContext{CurrentPagePermalink: tc.baseURL})
			html, err := renderDocHTML(t, doc, source)
			tc.assertResult(t, html, err)
		})
	}
}

func TestTransformExternalLinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		markdown       string
		addTargetBlank bool
		assertResult   func(t *testing.T, html string, err error)
	}{
		{
			name:           "adds attrs for external links",
			markdown:       "[ext](https://example.org) [internal](/x)",
			addTargetBlank: true,
			assertResult: func(t *testing.T, html string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, html, `href="https://example.org"`)
				assert.Contains(t, html, `target="_blank"`)
				assert.Contains(t, html, `rel="noopener"`)
				assert.Contains(t, html, `href="/x"`)
			},
		},
		{
			name:           "does not add attrs when disabled",
			markdown:       "[ext](https://example.org)",
			addTargetBlank: false,
			assertResult: func(t *testing.T, html string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, html, `href="https://example.org"`)
				assert.NotContains(t, html, `target="_blank"`)
				assert.NotContains(t, html, `rel="noopener"`)
			},
		},
		{
			name:           "keeps order for multiple externals",
			markdown:       "[a](https://a.example) [b](https://b.example)",
			addTargetBlank: true,
			assertResult: func(t *testing.T, html string, err error) {
				assert.NoError(t, err)
				assert.True(t, strings.Index(html, `href="https://a.example"`) < strings.Index(html, `href="https://b.example"`))
				assert.Equal(t, 2, strings.Count(html, `target="_blank"`))
				assert.Equal(t, 2, strings.Count(html, `rel="noopener"`))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, source := parseDoc(t, tc.markdown)
			_ = transformExternalLinks(doc, tc.addTargetBlank)
			html, err := renderDocHTML(t, doc, source)
			tc.assertResult(t, html, err)
		})
	}
}
