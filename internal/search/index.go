package search

import (
	"encoding/json"
	stdhtml "html"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/abdusco/kopkop/internal/content"
	"github.com/samber/lo"
	"golang.org/x/net/html"
)

type Entry struct {
	Title     string `json:"title"`
	Permalink string `json:"permalink"`
	Summary   string `json:"summary"`
	Content   string `json:"content"`
}

func BuildIndex(lib *content.Library, outputPath string, filename string) error {
	var entries []Entry
	permalinkSeen := map[string]struct{}{}
	paths := lo.Keys(lib.Pages)
	sort.Strings(paths)
	for _, p := range paths {
		pg := lib.Pages[p]
		summary := ""
		if pg.Summary != nil {
			summary = *pg.Summary
		}
		entries = append(entries, Entry{
			Title:     pg.Meta.Title,
			Permalink: pg.Permalink,
			Summary:   summary,
			Content:   stripTags(pg.Content),
		})
		permalinkSeen[pg.Permalink] = struct{}{}
	}

	sectionPaths := lo.Keys(lib.Sections)
	sort.Strings(sectionPaths)
	for _, p := range sectionPaths {
		sec := lib.Sections[p]
		if _, exists := permalinkSeen[sec.Permalink]; exists {
			continue
		}
		entries = append(entries, Entry{
			Title:     sec.Meta.Title,
			Permalink: sec.Permalink,
			Content:   stripTags(sec.Content),
		})
		permalinkSeen[sec.Permalink] = struct{}{}
	}

	b, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	dst := filepath.Join(outputPath, filename)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

func stripTags(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		replacer := strings.NewReplacer("<", " ", ">", " ", "\n", " ", "\t", " ")
		out := replacer.Replace(s)
		return strings.Join(strings.Fields(out), " ")
	}

	var b strings.Builder
	var walk func(n *html.Node, hidden bool)
	walk = func(n *html.Node, hidden bool) {
		if n == nil {
			return
		}
		nowHidden := hidden || isHiddenElement(n)
		if n.Type == html.TextNode && !nowHidden {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, nowHidden)
		}
	}
	walk(doc, false)

	out := stdhtml.UnescapeString(b.String())
	return strings.Join(strings.Fields(out), " ")
}

func isHiddenElement(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch strings.ToLower(n.Data) {
	case "script", "style", "noscript", "template":
		return true
	default:
		return false
	}
}
