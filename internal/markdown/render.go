package markdown

import (
	"bytes"
	"fmt"
	stdhtml "html"
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
var highlightedCodeBlockRe = regexp.MustCompile(`(?s)<pre data-lang="([^"]+)" class="language-[^"]*">\s*<code class="language-[^"]*" data-lang="[^"]*">(.*?)</code>\s*</pre>`)

type Heading struct {
	ID    string
	Level int
	Title string
}

type Rendered struct {
	Body          string
	Summary       *string
	TOC           []Heading
	ExternalLinks []string
}

type RenderContext struct {
	Permalinks               map[string]string
	CurrentPagePath          string
	CurrentPagePermalink     string
	InsertAnchorLinks        bool
	ExternalLinksTargetBlank bool
	HighlightTheme           string
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

	applyHeadingIDs(doc, source)

	externalLinks := transformExternalLinks(doc, ctx.ExternalLinksTargetBlank)
	if err := transformInternalLinks(doc, ctx); err != nil {
		return Rendered{}, err
	}
	transformColocatedAssetLinks(doc, ctx)

	buf := bytes.NewBuffer(nil)
	if err := md.Renderer().Render(buf, source, doc); err != nil {
		return Rendered{}, fmt.Errorf("render markdown: %w", err)
	}
	body := buf.String()
	body = continueReadingParagraphRe.ReplaceAllString(body, continueReadingHTML)
	body = fencedCodeLangRe.ReplaceAllString(body, `<pre data-lang="$1" class="language-$1 "><code class="language-$1" data-lang="$1">`)
	if strings.TrimSpace(ctx.HighlightTheme) != "" {
		body = applySyntaxHighlight(body, ctx.HighlightTheme)
	}
	if ctx.InsertAnchorLinks {
		body = insertAnchorLinks(body)
	}
	summary := extractSummary(content, ctx, md)

	toc := collectTOC(doc, source)

	return Rendered{
		Body:          body,
		Summary:       summary,
		TOC:           toc,
		ExternalLinks: externalLinks,
	}, nil
}

func applyHeadingIDs(doc ast.Node, source []byte) {
	headingIDCounts := map[string]int{}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		h, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}

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
		return ast.WalkContinue, nil
	})
}

func transformInternalLinks(doc ast.Node, ctx RenderContext) error {
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch node := n.(type) {
		case *ast.Link:
			dest := string(node.Destination)
			if !strings.HasPrefix(dest, "@/") {
				break
			}
			resolved, err := resolveInternalLink(dest, ctx)
			if err != nil {
				return ast.WalkStop, err
			}
			node.Destination = []byte(resolved)
		case *ast.Image:
			dest := string(node.Destination)
			if !strings.HasPrefix(dest, "@/") {
				break
			}
			resolved, err := resolveInternalLink(dest, ctx)
			if err != nil {
				return ast.WalkStop, err
			}
			node.Destination = []byte(resolved)
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return err
	}
	return nil
}

func transformColocatedAssetLinks(doc ast.Node, ctx RenderContext) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch node := n.(type) {
		case *ast.Link:
			dest := string(node.Destination)
			if isColocatedAssetLink(dest) {
				node.Destination = []byte(resolveColocatedAssetLink(dest, ctx))
			}
		case *ast.Image:
			dest := string(node.Destination)
			if isColocatedAssetLink(dest) {
				node.Destination = []byte(resolveColocatedAssetLink(dest, ctx))
			}
		}
		return ast.WalkContinue, nil
	})
}

func transformExternalLinks(doc ast.Node, addTargetBlank bool) []string {
	var externalLinks []string
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		node, ok := n.(*ast.Link)
		if !ok {
			return ast.WalkContinue, nil
		}

		link := string(node.Destination)
		if !isExternalLink(link) {
			return ast.WalkContinue, nil
		}

		externalLinks = append(externalLinks, link)
		if addTargetBlank {
			node.SetAttributeString("target", []byte("_blank"))
			node.SetAttributeString("rel", []byte("noopener"))
		}
		return ast.WalkContinue, nil
	})
	return externalLinks
}

func collectTOC(doc ast.Node, source []byte) []Heading {
	var toc []Heading
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
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
			Title: nodePlainText(h, source),
		})
		return ast.WalkContinue, nil
	})
	return toc
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

func headingNodeText(h *ast.Heading, source []byte) string {
	return nodePlainText(h, source)
}

func nodePlainText(n ast.Node, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(curr ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch x := curr.(type) {
		case *ast.Text:
			b.Write(x.Segment.Value(source))
		case *ast.String:
			b.Write(x.Text(source))
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

func applySyntaxHighlight(htmlIn string, themeName string) string {
	formatter := chromahtml.New(
		chromahtml.WithClasses(true),
		chromahtml.ClassPrefix("z-"),
		chromahtml.PreventSurroundingPre(true),
	)
	style := styles.Get(strings.TrimSpace(themeName))
	if style == nil {
		style = styles.Get("github")
	}
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
		return `<pre data-lang="` + lang + `" data-highlighted="true" class="language-` + lang + ` z-code z-chroma"><code class="language-` + lang + `" data-lang="` + lang + `">` + highlighted + `</code></pre>`
	})
}

func HighlightCSS(themeName string) (string, error) {
	name := strings.TrimSpace(themeName)
	if name == "" {
		return "", fmt.Errorf("highlight theme must not be empty")
	}
	style := styles.Get(name)
	if style == nil {
		style = styles.Get("github")
	}
	if style == nil {
		style = styles.Fallback
	}
	formatter := chromahtml.New(
		chromahtml.WithClasses(true),
		chromahtml.ClassPrefix("z-"),
	)
	var out bytes.Buffer
	if err := formatter.WriteCSS(&out, style); err != nil {
		return "", fmt.Errorf("write highlight css: %w", err)
	}
	css := strings.TrimSpace(out.String())
	if css == "" {
		return "", fmt.Errorf("highlight css is empty")
	}
	return css + "\n", nil
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

func resolveInternalLink(link string, ctx RenderContext) (resolved string, err error) {
	mdPath, hash := splitLinkAnchor(strings.TrimPrefix(link, "@/"))
	permalink, ok := ctx.Permalinks[mdPath]
	if !ok {
		return "", fmt.Errorf("broken relative link %q", link)
	}

	full := permalink
	if hash != nil {
		full = permalink + "#" + *hash
	}
	return full, nil
}

func resolveColocatedAssetLink(link string, ctx RenderContext) string {
	base := strings.TrimRight(ctx.CurrentPagePermalink, "/")
	if base == "" {
		return link
	}
	return base + "/" + link
}

func isExternalLink(link string) bool {
	return strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://")
}

func splitLinkAnchor(link string) (string, *string) {
	before, hash, ok := strings.Cut(link, "#")
	if !ok {
		clean := path.Clean(link)
		return strings.TrimPrefix(clean, "/"), nil
	}
	p := strings.TrimPrefix(path.Clean(before), "/")
	return p, &hash
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
