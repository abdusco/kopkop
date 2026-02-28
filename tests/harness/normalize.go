package harness

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

var wsRe = regexp.MustCompile(`\s+`)
var translatedInMarkerRe = regexp.MustCompile(`Translated in [^:]+:`)
var articleBlockRe = regexp.MustCompile(`(?s)<article>.*?</article>`)
var simpleSearchJSRe = regexp.MustCompile(`(?s)window\.searchIndex\[[^\]]+\]\s*=\s*(\[.*\])\s*;?`)
var lunrSearchJSRe = regexp.MustCompile(`(?s)window\.searchIndex\s*=\s*(\{.*\})\s*;?`)

func NormalizeByExt(ext string, in []byte) ([]byte, error) {
	switch strings.ToLower(ext) {
	case ".json":
		return normalizeJSON(in)
	case ".html", ".htm":
		return normalizeHTML(in), nil
	case ".xml":
		return normalizeXML(in)
	case ".js":
		return normalizeJS(in), nil
	default:
		return bytes.TrimSpace(in), nil
	}
}

func normalizeJSON(in []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(in, &v); err != nil {
		return nil, fmt.Errorf("normalize json: %w", err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("normalize json marshal: %w", err)
	}
	return out, nil
}

func normalizeHTML(in []byte) []byte {
	s := string(in)
	s = normalizeListPostsBlocks(s)
	s = normalizeTranslationBlocks(s)
	s = strings.TrimSpace(s)
	s = wsRe.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "> <", "><")
	return []byte(s)
}

func normalizeListPostsBlocks(s string) string {
	const startMarker = `<div class="list-posts">`
	idx := 0
	for {
		startRel := strings.Index(s[idx:], startMarker)
		if startRel == -1 {
			return s
		}
		start := idx + startRel + len(startMarker)
		endRel := strings.Index(s[start:], `</div>`)
		if endRel == -1 {
			return s
		}
		end := start + endRel
		inner := s[start:end]
		blocks := articleBlockRe.FindAllString(inner, -1)
		if len(blocks) > 1 {
			sort.Strings(blocks)
			rebuilt := "\n" + strings.Join(blocks, "\n") + "\n"
			s = s[:start] + rebuilt + s[end:]
			idx = start + len(rebuilt)
			continue
		}
		idx = end + len(`</div>`)
	}
}

func normalizeTranslationBlocks(s string) string {
	idxs := translatedInMarkerRe.FindAllStringIndex(s, -1)
	if len(idxs) < 2 {
		return s
	}
	prefix := s[:idxs[0][0]]
	chunks := make([]string, 0, len(idxs))
	for i := 0; i < len(idxs); i++ {
		start := idxs[i][0]
		end := len(s)
		if i+1 < len(idxs) {
			end = idxs[i+1][0]
		}
		chunks = append(chunks, strings.TrimSpace(s[start:end]))
	}
	sort.Strings(chunks)
	return prefix + strings.Join(chunks, "\n")
}

func normalizeXML(in []byte) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(in))
	var tokens []xml.Token
	for {
		tok, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("normalize xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.ProcInst, xml.Directive:
			continue
		case xml.CharData:
			trimmed := strings.TrimSpace(string(t))
			if trimmed == "" {
				continue
			}
			tokens = append(tokens, xml.CharData(trimmed))
		default:
			tokens = append(tokens, tok)
		}
	}

	buf := bytes.NewBuffer(nil)
	encoder := xml.NewEncoder(buf)
	for _, tok := range tokens {
		if err := encoder.EncodeToken(tok); err != nil {
			return nil, fmt.Errorf("normalize xml encode: %w", err)
		}
	}
	if err := encoder.Flush(); err != nil {
		return nil, fmt.Errorf("normalize xml flush: %w", err)
	}
	return buf.Bytes(), nil
}

func normalizeJS(in []byte) []byte {
	s := strings.TrimSpace(string(in))
	if strings.Contains(s, "elasticlunr") {
		return []byte("elasticlunr")
	}
	if !strings.Contains(s, "searchIndex") {
		return []byte(s)
	}
	if m := simpleSearchJSRe.FindStringSubmatch(s); len(m) == 2 {
		type searchEntry struct {
			Title     string `json:"title"`
			Permalink string `json:"permalink"`
		}
		var entries []searchEntry
		if err := json.Unmarshal([]byte(m[1]), &entries); err != nil {
			return []byte(s)
		}
		vals := make([]string, 0, len(entries))
		for _, e := range entries {
			vals = append(vals, e.Permalink+"|"+e.Title)
		}
		sort.Strings(vals)
		return []byte(strings.Join(vals, "\n"))
	}

	if m := lunrSearchJSRe.FindStringSubmatch(s); len(m) == 2 {
		var payload map[string]any
		if err := json.Unmarshal([]byte(m[1]), &payload); err != nil {
			return []byte(s)
		}
		documentStore, _ := payload["documentStore"].(map[string]any)
		docs, _ := documentStore["docs"].(map[string]any)
		if len(docs) == 0 {
			return []byte(s)
		}
		vals := make([]string, 0, len(docs))
		for id, rawDoc := range docs {
			title := ""
			if doc, ok := rawDoc.(map[string]any); ok {
				if t, ok := doc["title"].(string); ok {
					title = t
				}
			}
			vals = append(vals, id+"|"+title)
		}
		sort.Strings(vals)
		return []byte(strings.Join(vals, "\n"))
	}

	return []byte(s)
}
