package markdown

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

func parseDoc(t *testing.T, in string) (ast.Node, []byte) {
	t.Helper()
	md := goldmark.New()
	source := []byte(in)
	doc := md.Parser().Parse(text.NewReader(source))
	return doc, source
}

func firstLinkNode(t *testing.T, doc ast.Node) *ast.Link {
	t.Helper()
	var out *ast.Link
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if link, ok := n.(*ast.Link); ok {
			out = link
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	require.NotNil(t, out)
	return out
}

func firstImageNode(t *testing.T, doc ast.Node) *ast.Image {
	t.Helper()
	var out *ast.Image
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if img, ok := n.(*ast.Image); ok {
			out = img
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	require.NotNil(t, out)
	return out
}

func TestRenderContent_SummaryDivider(t *testing.T) {
	t.Parallel()

	ctx := RenderContext{}
	res, err := RenderContent("hello\n\n<!-- more -->\n\nworld", ctx)
	require.NoError(t, err)

	assert.Contains(t, res.Body, "continue-reading")
	require.NotNil(t, res.Summary)
	assert.Contains(t, *res.Summary, "<p>hello</p>")
}

func TestRenderContent_DuplicateHeadingAnchors(t *testing.T) {
	t.Parallel()

	ctx := RenderContext{}
	res, err := RenderContent("# Example\n\n# Example\n", ctx)
	require.NoError(t, err)

	assert.Contains(t, res.Body, `id="example"`)
	assert.Contains(t, res.Body, `id="example-1"`)
	require.Len(t, res.TOC, 2)
	assert.Equal(t, "example", res.TOC[0].ID)
	assert.Equal(t, "example-1", res.TOC[1].ID)
}

func TestRenderContent_HeadingTextIncludesInlineCode(t *testing.T) {
	t.Parallel()

	res, err := RenderContent("# text here `with code`", RenderContext{})
	require.NoError(t, err)

	require.Len(t, res.TOC, 1)
	assert.Equal(t, "text here with code", res.TOC[0].Title)
	assert.Equal(t, "text-here-with-code", res.TOC[0].ID)
	assert.Contains(t, res.Body, `id="text-here-with-code"`)
}

func TestRenderContent_InternalAndExternalLinks(t *testing.T) {
	t.Parallel()

	ctx := RenderContext{
		Permalinks: map[string]string{
			"content/posts/hello.md": "https://example.com/posts/hello/",
		},
		CurrentPagePermalink: "https://example.com/posts/current/",
	}

	in := "[internal](@/content/posts/hello.md#intro) [ext](https://example.org) [asset](image.png)"
	res, err := RenderContent(in, ctx)
	require.NoError(t, err)

	assert.Contains(t, res.Body, "https://example.com/posts/hello/#intro")
	assert.Contains(t, res.Body, "https://example.com/posts/current/image.png")
	assert.Equal(t, []string{"https://example.org"}, res.ExternalLinks)
}

func TestRenderContent_BrokenInternalLinkErrors(t *testing.T) {
	t.Parallel()

	ctx := RenderContext{
		Permalinks: map[string]string{},
	}
	_, err := RenderContent("[broken](@/content/posts/missing.md)", ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken relative link")
}

func TestRenderContent_AnchorLinksToggle(t *testing.T) {
	t.Parallel()

	resWithoutAnchors, err := RenderContent("# Heading", RenderContext{InsertAnchorLinks: false})
	require.NoError(t, err)
	assert.NotContains(t, resWithoutAnchors.Body, "zola-anchor")

	resWithAnchors, err := RenderContent("# Heading", RenderContext{InsertAnchorLinks: true})
	require.NoError(t, err)
	assert.Contains(t, resWithAnchors.Body, "zola-anchor")
}

func TestRenderContent_ContinueReadingIsNotWrappedInParagraph(t *testing.T) {
	t.Parallel()

	res, err := RenderContent("Before\n\n<!-- more -->\n\nAfter", RenderContext{})
	require.NoError(t, err)
	assert.Contains(t, res.Body, `<span id="continue-reading"></span>`)
	assert.NotContains(t, res.Body, `<p><span id="continue-reading"></span></p>`)
}

func TestRenderContent_FencedCodeHasZolaLikeLanguageAttributes(t *testing.T) {
	t.Parallel()

	res, err := RenderContent("```rust\nfn main() {}\n```", RenderContext{})
	require.NoError(t, err)
	assert.Contains(t, res.Body, `<pre data-lang="rust" class="language-rust "><code class="language-rust" data-lang="rust">`)
}

func TestRenderContent_HighlightedCodeAddsZCodeSpans(t *testing.T) {
	t.Parallel()

	res, err := RenderContent("```go\npackage main\n```", RenderContext{HighlightCode: true})
	require.NoError(t, err)
	assert.Contains(t, res.Body, `class="language-go z-code"`)
	assert.Contains(t, res.Body, `<span class="z-`)
}

func TestRenderContent_ExternalLinksTargetBlankUsesASTAttributes(t *testing.T) {
	t.Parallel()

	res, err := RenderContent("[ext](https://example.org)", RenderContext{ExternalLinksTargetBlank: true})
	require.NoError(t, err)
	assert.Contains(t, res.Body, `href="https://example.org"`)
	assert.Contains(t, res.Body, `target="_blank"`)
	assert.Contains(t, res.Body, `rel="noopener"`)
}

func TestTransformInternalLinks_Rewrites(t *testing.T) {
	t.Parallel()

	doc, _ := parseDoc(t, "[internal](@/content/posts/hello.md#intro) ![img](@/content/posts/hello.md)")
	ctx := RenderContext{Permalinks: map[string]string{"content/posts/hello.md": "https://example.com/posts/hello/"}}

	err := transformInternalLinks(doc, ctx)
	require.NoError(t, err)

	link := firstLinkNode(t, doc)
	img := firstImageNode(t, doc)
	assert.Equal(t, "https://example.com/posts/hello/#intro", string(link.Destination))
	assert.Equal(t, "https://example.com/posts/hello/", string(img.Destination))
}

func TestTransformInternalLinks_BrokenReturnsError(t *testing.T) {
	t.Parallel()

	doc, _ := parseDoc(t, "[broken](@/content/posts/missing.md)")
	err := transformInternalLinks(doc, RenderContext{Permalinks: map[string]string{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken relative link")
}

func TestTransformExternalLinks_CollectsAndAppliesAttrs(t *testing.T) {
	t.Parallel()

	doc, _ := parseDoc(t, "[ext](https://example.org)")
	links := transformExternalLinks(doc, true)
	assert.Equal(t, []string{"https://example.org"}, links)

	link := firstLinkNode(t, doc)
	target, hasTarget := link.AttributeString("target")
	rel, hasRel := link.AttributeString("rel")
	assert.True(t, hasTarget)
	assert.True(t, hasRel)
	assert.Equal(t, "_blank", string(target.([]byte)))
	assert.Equal(t, "noopener", string(rel.([]byte)))
}
