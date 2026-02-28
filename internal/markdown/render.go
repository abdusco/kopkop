package markdown

import (
	"bytes"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

const continueReadingHTML = `<span id="continue-reading"></span>`

var moreDividerRe = regexp.MustCompile(`(?is)<!--\s*more\s*-->`)

type Heading struct {
	ID    string
	Level int
	Title string
}

type InternalLink struct {
	Path   string
	Anchor *string
}

type Rendered struct {
	Body          string
	Summary       *string
	TOC           []Heading
	InternalLinks []InternalLink
	ExternalLinks []string
}

type RenderContext struct {
	Permalinks           map[string]string
	CurrentPagePath      string
	CurrentPagePermalink string
}

func RenderContent(content string, ctx RenderContext) (Rendered, error) {
	md := goldmark.New(
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithUnsafe(),
		),
	)

	contentWithMarker := moreDividerRe.ReplaceAllString(content, continueReadingHTML)
	source := []byte(contentWithMarker)
	pc := parser.NewContext()
	doc := md.Parser().Parse(text.NewReader(source), parser.WithContext(pc))

	internalLinks := make([]InternalLink, 0)
	externalLinks := make([]string, 0)

	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		link, ok := n.(*ast.Link)
		if !ok {
			return ast.WalkContinue, nil
		}

		dest := string(link.Destination)
		resolved, internal, external, err := resolveLink(dest, ctx)
		if err != nil {
			return ast.WalkStop, err
		}
		if internal != nil {
			internalLinks = append(internalLinks, *internal)
		}
		if external != "" {
			externalLinks = append(externalLinks, external)
		}
		link.Destination = []byte(resolved)
		return ast.WalkContinue, nil
	})
	if err != nil {
		return Rendered{}, err
	}

	buf := bytes.NewBuffer(nil)
	if err := md.Renderer().Render(buf, source, doc); err != nil {
		return Rendered{}, fmt.Errorf("render markdown: %w", err)
	}
	body := buf.String()
	summary := extractSummary(content, ctx, md)

	toc := make([]Heading, 0)
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		h, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}
		idRaw, ok := h.AttributeString("id")
		id := ""
		if ok {
			switch v := idRaw.(type) {
			case []byte:
				id = string(v)
			case string:
				id = v
			}
		}
		toc = append(toc, Heading{
			ID:    id,
			Level: h.Level,
			Title: headingText(h, source),
		})
		return ast.WalkContinue, nil
	})

	return Rendered{
		Body:          body,
		Summary:       summary,
		TOC:           toc,
		InternalLinks: internalLinks,
		ExternalLinks: externalLinks,
	}, nil
}

func extractSummary(content string, ctx RenderContext, md goldmark.Markdown) *string {
	loc := moreDividerRe.FindStringIndex(content)
	if loc == nil {
		return nil
	}
	before := content[:loc[0]]
	buf := bytes.NewBuffer(nil)
	doc := md.Parser().Parse(text.NewReader([]byte(before)))
	if err := md.Renderer().Render(buf, []byte(before), doc); err != nil {
		return nil
	}
	s := buf.String() + continueReadingHTML
	return &s
}

func headingText(h *ast.Heading, source []byte) string {
	var b strings.Builder
	for c := h.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			b.Write(t.Segment.Value(source))
		}
	}
	return b.String()
}

func resolveLink(link string, ctx RenderContext) (resolved string, internal *InternalLink, external string, err error) {
	if strings.HasPrefix(link, "@/") {
		mdPath, anchor := splitLinkAnchor(strings.TrimPrefix(link, "@/"))
		permalink, ok := ctx.Permalinks[mdPath]
		if !ok {
			return "", nil, "", fmt.Errorf("broken relative link %q", link)
		}
		full := permalink
		if anchor != nil {
			full = permalink + "#" + *anchor
		}
		return full, &InternalLink{Path: mdPath, Anchor: anchor}, "", nil
	}

	if isColocatedAssetLink(link) {
		base := strings.TrimRight(ctx.CurrentPagePermalink, "/")
		if base == "" {
			return link, nil, "", nil
		}
		return base + "/" + link, nil, "", nil
	}

	if isExternalLink(link) {
		return link, nil, link, nil
	}

	return link, nil, "", nil
}

func isExternalLink(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func splitLinkAnchor(link string) (string, *string) {
	idx := strings.IndexByte(link, '#')
	if idx == -1 {
		clean := path.Clean(link)
		return strings.TrimPrefix(clean, "/"), nil
	}
	p := strings.TrimPrefix(path.Clean(link[:idx]), "/")
	a := link[idx+1:]
	return p, &a
}

func isColocatedAssetLink(link string) bool {
	return !strings.HasPrefix(link, "/") &&
		!strings.HasPrefix(link, "..") &&
		!strings.HasPrefix(link, "#") &&
		!looksLikeSchema(link)
}

func looksLikeSchema(link string) bool {
	if idx := strings.IndexByte(link, ':'); idx > 0 {
		for _, r := range link[:idx] {
			if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '-') {
				return false
			}
		}
		return true
	}
	return false
}
