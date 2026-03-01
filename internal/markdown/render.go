package markdown

import (
	"bytes"
	"fmt"
	stdhtml "html"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	ghtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

const continueReadingHTML = `<span id="continue-reading"></span>`

var moreDividerRe = regexp.MustCompile(`(?is)<!--\s*more\s*-->`)
var headingRe = regexp.MustCompile(`(?s)<h([1-6]) id="([^"]+)">(.*?)</h[1-6]>`)
var continueReadingParagraphRe = regexp.MustCompile(`(?s)<p>\s*` + regexp.QuoteMeta(continueReadingHTML) + `\s*</p>`)
var fencedCodeLangRe = regexp.MustCompile(`<pre><code class="language-([^"]+)">`)
var externalAnchorStartRe = regexp.MustCompile(`<a\s+([^>]*?)href="(https?://[^"]+)"([^>]*)>`)
var highlightedCodeBlockRe = regexp.MustCompile(`(?s)<pre data-lang="([^"]+)" class="language-[^"]*">\s*<code class="language-[^"]*" data-lang="[^"]*">(.*?)</code>\s*</pre>`)

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
	Permalinks               map[string]string
	CurrentPagePath          string
	CurrentPagePermalink     string
	InsertAnchorLinks        bool
	ExternalLinksTargetBlank bool
	HighlightCode            bool
}

func RenderContent(content string, ctx RenderContext) (Rendered, error) {
	md := goldmark.New(
		goldmark.WithRendererOptions(
			ghtml.WithUnsafe(),
			ghtml.WithXHTML(),
		),
	)

	contentWithMarker := moreDividerRe.ReplaceAllString(content, continueReadingHTML)
	source := []byte(contentWithMarker)
	pc := parser.NewContext()
	doc := md.Parser().Parse(text.NewReader(source), parser.WithContext(pc))

	internalLinks := make([]InternalLink, 0)
	externalLinks := make([]string, 0)
	headingIDCounts := map[string]int{}

	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch h := n.(type) {
		case *ast.Heading:
			text := headingNodeText(h, source)
			id := slugifyHeadingID(text)
			if id == "" {
				id = "section"
			}
			if count, exists := headingIDCounts[id]; exists {
				headingIDCounts[id] = count + 1
				id = fmt.Sprintf("%s-%d", id, count)
			} else {
				headingIDCounts[id] = 1
			}
			h.SetAttributeString("id", []byte(id))
		}

		switch node := n.(type) {
		case *ast.Link:
			dest := string(node.Destination)
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
			node.Destination = []byte(resolved)
		case *ast.Image:
			dest := string(node.Destination)
			resolved, _, _, err := resolveLink(dest, ctx)
			if err != nil {
				return ast.WalkStop, err
			}
			node.Destination = []byte(resolved)
		}
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
	body = continueReadingParagraphRe.ReplaceAllString(body, continueReadingHTML)
	body = fencedCodeLangRe.ReplaceAllString(body, `<pre data-lang="$1" class="language-$1 "><code class="language-$1" data-lang="$1">`)
	if ctx.HighlightCode {
		body = applySyntaxHighlight(body)
	}
	if ctx.InsertAnchorLinks {
		body = insertAnchorLinks(body)
	}
	if ctx.ExternalLinksTargetBlank {
		body = addTargetBlankToExternalLinks(body)
	}
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

func insertAnchorLinks(htmlIn string) string {
	return headingRe.ReplaceAllStringFunc(htmlIn, func(m string) string {
		sub := headingRe.FindStringSubmatch(m)
		if len(sub) != 4 {
			return m
		}
		level := sub[1]
		id := sub[2]
		inner := sub[3]
		label := strings.ToLower(id)
		anchor := `<a class="zola-anchor" href="#` + id + `" aria-label="Anchor link for: ` + label + `">🔗</a>`
		return `<h` + level + ` id="` + id + `">` + inner + anchor + `</h` + level + `>`
	})
}

func addTargetBlankToExternalLinks(htmlIn string) string {
	return externalAnchorStartRe.ReplaceAllStringFunc(htmlIn, func(m string) string {
		sub := externalAnchorStartRe.FindStringSubmatch(m)
		if len(sub) != 4 {
			return m
		}
		before := sub[1]
		href := sub[2]
		after := sub[3]
		attrs := strings.TrimSpace(before + " " + after)
		target := ""
		rel := ""
		if strings.Contains(attrs, `target=`) {
			target = ""
		} else {
			target = ` target="_blank"`
		}
		if strings.Contains(attrs, `rel=`) {
			rel = ""
		} else {
			rel = ` rel="noopener"`
		}
		if attrs != "" {
			attrs = " " + attrs
		}
		return `<a` + attrs + rel + target + ` href="` + href + `">`
	})
}

func headingNodeText(h *ast.Heading, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(h, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch x := n.(type) {
		case *ast.Text:
			b.Write(x.Segment.Value(source))
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(b.String())
}

func slugifyHeadingID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	lastHyphen := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen {
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	out := strings.Trim(b.String(), "-")
	return out
}

func applySyntaxHighlight(htmlIn string) string {
	formatter := chromahtml.New(
		chromahtml.WithClasses(true),
		chromahtml.ClassPrefix("z-"),
		chromahtml.PreventSurroundingPre(true),
	)
	style := styles.Get("github")
	if style == nil {
		style = styles.Fallback
	}

	return highlightedCodeBlockRe.ReplaceAllStringFunc(htmlIn, func(block string) string {
		sub := highlightedCodeBlockRe.FindStringSubmatch(block)
		if len(sub) != 3 {
			return block
		}
		lang := sub[1]
		rawCode := stdhtml.UnescapeString(sub[2])

		lexer := lexers.Get(lang)
		if lexer == nil {
			lexer = lexers.Analyse(rawCode)
		}
		if lexer == nil {
			lexer = lexers.Fallback
		}
		lexer = chroma.Coalesce(lexer)

		iterator, err := lexer.Tokenise(nil, rawCode)
		if err != nil {
			return block
		}
		var out bytes.Buffer
		if err := formatter.Format(&out, style, iterator); err != nil {
			return block
		}
		highlighted := strings.TrimRight(out.String(), "\n")
		return `<pre data-lang="` + lang + `" class="language-` + lang + ` z-code"><code class="language-` + lang + `" data-lang="` + lang + `">` + highlighted + `</code></pre>`
	})
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
