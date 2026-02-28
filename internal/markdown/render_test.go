package markdown

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	require.Len(t, res.InternalLinks, 1)
	assert.Equal(t, "content/posts/hello.md", res.InternalLinks[0].Path)
	require.NotNil(t, res.InternalLinks[0].Anchor)
	assert.Equal(t, "intro", *res.InternalLinks[0].Anchor)
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
