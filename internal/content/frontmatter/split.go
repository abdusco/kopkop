package frontmatter

import (
	"fmt"
	"regexp"

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

var (
	tomlRe = regexp.MustCompile(`(?s)^[\s]*\+\+\+\r?\n(.*?)\r?\n\+\+\+[\s]*(?:$|\r?\n(.*)$)`)
	yamlRe = regexp.MustCompile(`(?s)^[\s]*---\r?\n(.*?)\r?\n---[\s]*(?:$|\r?\n(.*)$)`)
)

func SplitContent(filePath string, content string) (RawFrontMatter, string, error) {
	if matches := tomlRe.FindStringSubmatch(content); matches != nil {
		body := ""
		if len(matches) > 2 {
			body = matches[2]
		}
		return RawFrontMatter{Format: FormatTOML, Data: matches[1]}, body, nil
	}

	if matches := yamlRe.FindStringSubmatch(content); matches != nil {
		body := ""
		if len(matches) > 2 {
			body = matches[2]
		}
		return RawFrontMatter{Format: FormatYAML, Data: matches[1]}, body, nil
	}

	return RawFrontMatter{}, "", fmt.Errorf("couldn't find front matter in %q; expected +++ or ---", filePath)
}

func (r RawFrontMatter) Decode(v any) error {
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
