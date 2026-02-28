package markdown

import (
	"strings"
	"testing"
)

func BenchmarkRenderContent(b *testing.B) {
	ctx := RenderContext{
		Permalinks:           map[string]string{"content/posts/one.md": "https://example.com/posts/one/"},
		CurrentPagePermalink: "https://example.com/posts/current/",
	}

	input := strings.Repeat("# Heading\n\nParagraph with [link](@/content/posts/one.md#x) and [ext](https://example.org).\n\n", 30)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = RenderContent(input, ctx)
	}
}
