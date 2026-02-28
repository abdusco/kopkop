package harness

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var wsRe = regexp.MustCompile(`\s+`)

func NormalizeByExt(ext string, in []byte) ([]byte, error) {
	switch strings.ToLower(ext) {
	case ".json":
		return normalizeJSON(in)
	case ".html", ".htm":
		return normalizeHTML(in), nil
	case ".xml":
		return normalizeXML(in)
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
	s = strings.TrimSpace(s)
	s = wsRe.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "> <", "><")
	return []byte(s)
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
