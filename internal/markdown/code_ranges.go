package markdown

import (
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// CodeRanges identifies source regions that must stay literal during expression
// and shortcode expansion. Rendering parses the expanded Markdown separately.
func CodeRanges(content string) [][2]int {
	if !strings.Contains(content, "{{") && !strings.Contains(content, "{%") {
		return nil
	}
	source := []byte(content)
	doc := markdownEngine.Parser().Parse(text.NewReader(source))
	var ranges [][2]int
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := n.(type) {
		case *ast.CodeSpan:
			first, last := node.FirstChild(), node.LastChild()
			if first != nil {
				ranges = append(ranges, [2]int{first.(*ast.Text).Segment.Start, last.(*ast.Text).Segment.Stop})
			}
			return ast.WalkSkipChildren, nil
		case *ast.FencedCodeBlock:
			if node.Info != nil {
				ranges = append(ranges, [2]int{node.Info.Segment.Start, node.Info.Segment.Stop})
			}
		case *ast.CodeBlock:
		default:
			return ast.WalkContinue, nil
		}
		lines := n.Lines()
		if lines.Len() > 0 {
			ranges = append(ranges, [2]int{lines.At(0).Start, lines.At(lines.Len() - 1).Stop})
		}
		return ast.WalkSkipChildren, nil
	})
	return ranges
}
