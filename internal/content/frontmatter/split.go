package frontmatter

import (
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

type Format string

const (
	FormatTOML Format = "toml"
	FormatYAML Format = "yaml"
)

type RawFrontMatter struct {
	Format Format
	Data   string
}

func SplitContent(filePath string, content string) (RawFrontMatter, string, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	start := firstNonWhitespace(normalized)
	if start < 0 {
		return RawFrontMatter{}, "", fmt.Errorf("couldn't find front matter in %q; expected +++ or ---", filePath)
	}

	chunk := normalized[start:]
	delim := ""
	format := Format("")
	switch {
	case strings.HasPrefix(chunk, "+++"):
		delim = "+++"
		format = FormatTOML
	case strings.HasPrefix(chunk, "---"):
		delim = "---"
		format = FormatYAML
	default:
		return RawFrontMatter{}, "", fmt.Errorf("couldn't find front matter in %q; expected +++ or ---", filePath)
	}

	afterOpen := chunk[len(delim):]
	if !strings.HasPrefix(afterOpen, "\n") {
		return RawFrontMatter{}, "", fmt.Errorf("couldn't find front matter in %q; expected +++ or ---", filePath)
	}
	afterOpen = afterOpen[1:]

	lines := strings.Split(afterOpen, "\n")
	header := make([]string, 0, 8)
	closeLine := -1
	for i, line := range lines {
		if line == delim {
			closeLine = i
			break
		}
		header = append(header, line)
	}
	if closeLine == -1 {
		return RawFrontMatter{}, "", fmt.Errorf("couldn't find front matter in %q; expected +++ or ---", filePath)
	}

	body := ""
	if closeLine+1 < len(lines) {
		body = strings.Join(lines[closeLine+1:], "\n")
	}

	return RawFrontMatter{Format: format, Data: strings.Join(header, "\n")}, body, nil
}

func firstNonWhitespace(s string) int {
	for i, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		return i
	}
	return -1
}

func (r RawFrontMatter) Decode(v any) error {
	if strings.TrimSpace(r.Data) == "" {
		return nil
	}
	switch r.Format {
	case FormatTOML:
		if _, err := toml.Decode(r.Data, v); err != nil {
			return fmt.Errorf("toml deserialize error: %w", err)
		}
		return nil
	case FormatYAML:
		if err := yaml.Unmarshal([]byte(r.Data), v); err != nil {
			return fmt.Errorf("yaml deserialize error: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported front matter format: %q", r.Format)
	}
}

func ParseFrontMatter[T any](filePath string, content string) (T, string, error) {
	var out T
	raw, body, err := SplitContent(filePath, content)
	if err != nil {
		return out, "", err
	}
	if err := raw.Decode(&out); err != nil {
		return out, "", fmt.Errorf("error parsing front matter for %q: %w", filePath, err)
	}
	return out, body, nil
}
