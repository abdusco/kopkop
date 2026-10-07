package frontmatter

import (
	"fmt"
	"regexp"
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

// FieldLine locates a top-level field in the original source, including the
// opening delimiter and leading whitespace. Zero means the field is absent.
func FieldLine(content, key string) int {
	// Most pages lack most keys; skip parsing the front matter again for them.
	if !strings.Contains(content, key) {
		return 0
	}
	content = strings.ReplaceAll(strings.TrimPrefix(content, "\ufeff"), "\r\n", "\n")
	raw, _, err := SplitContent(content)
	if err != nil {
		return 0
	}
	offset := strings.Count(content[:firstNonWhitespace(content)], "\n") + 1
	if raw.Format == FormatYAML {
		var document yaml.Node
		if err := yaml.Unmarshal([]byte(raw.Data), &document); err != nil || len(document.Content) == 0 {
			return 0
		}
		mapping := document.Content[0]
		if mapping.Kind != yaml.MappingNode {
			return 0
		}
		for i := 0; i < len(mapping.Content); i += 2 {
			if mapping.Content[i].Value == key {
				return offset + mapping.Content[i].Line
			}
		}
		return 0
	}
	var fields map[string]any
	if err := raw.Decode(&fields); err != nil {
		return 0
	}
	if _, exists := fields[key]; !exists {
		return 0
	}
	pattern := regexp.MustCompile(`^\s*(?:` + regexp.QuoteMeta(key) + `|"` + regexp.QuoteMeta(key) + `"|'` + regexp.QuoteMeta(key) + `')\s*=`)
	inTable := false
	for i, line := range strings.Split(raw.Data, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[") {
			if strings.HasPrefix(trim, "["+key+"]") || strings.HasPrefix(trim, "["+key+".") {
				return offset + i + 1
			}
			inTable = true
			continue
		}
		if !inTable && pattern.MatchString(line) {
			return offset + i + 1
		}
	}
	// Inline tables and unusual key syntax still have source-file context.
	return offset + 1
}

func SplitContent(content string) (RawFrontMatter, string, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	start := firstNonWhitespace(normalized)
	if start < 0 {
		return RawFrontMatter{}, "", fmt.Errorf("couldn't find front matter; expected +++ or ---")
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
		return RawFrontMatter{}, "", fmt.Errorf("couldn't find front matter; expected +++ or ---")
	}

	afterOpen := chunk[len(delim):]
	if !strings.HasPrefix(afterOpen, "\n") {
		return RawFrontMatter{}, "", fmt.Errorf("couldn't find front matter; expected +++ or ---")
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
		return RawFrontMatter{}, "", fmt.Errorf("couldn't find front matter; expected +++ or ---")
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
		if _, err := toml.Decode(utcLocalDates(r.Data), v); err != nil {
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
	raw, body, err := SplitContent(content)
	if err != nil {
		return out, "", fmt.Errorf("error parsing front matter for %q: %w", filePath, err)
	}
	if err := raw.Decode(&out); err != nil {
		return out, "", fmt.Errorf("error parsing front matter for %q: %w", filePath, err)
	}
	return out, body, nil
}

var localDateRe = regexp.MustCompile(`^(\s*(?:date|updated)\s*=\s*)(\d{4}-\d{2}-\d{2})(?:[T ](\d{2}:\d{2})(:\d{2}(?:\.\d+)?)?)?(\s*(?:#.*)?)$`)

// utcLocalDates pins offset-less top-level date/updated values to UTC. The TOML
// decoder would otherwise read them in the machine's local zone, which makes
// output depend on TZ.
func utcLocalDates(data string) string {
	lines := strings.Split(data, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			break
		}
		m := localDateRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		clock, seconds := m[3], m[4]
		if clock == "" {
			clock = "00:00"
		}
		if seconds == "" {
			seconds = ":00"
		}
		lines[i] = m[1] + m[2] + "T" + clock + seconds + "Z" + m[5]
	}
	return strings.Join(lines, "\n")
}
