package shortcode

import (
	"fmt"
	"strconv"
	"strings"
)

const Placeholder = "@@ZOLA_SC_PLACEHOLDER@@"

type Shortcode struct {
	Name  string
	Args  map[string]any
	Span  [2]int
	Body  *string
	Nth   int
	Inner []Shortcode
}

type invocationCounter struct {
	count map[string]int
}

func newInvocationCounter() *invocationCounter {
	return &invocationCounter{count: map[string]int{}}
}

func (c *invocationCounter) Next(name string) int {
	c.count[name]++
	return c.count[name]
}

func Parse(content string) (string, []Shortcode, error) {
	return parseWithCounter(content, newInvocationCounter())
}

func parseWithCounter(content string, counter *invocationCounter) (string, []Shortcode, error) {
	var out strings.Builder
	out.Grow(len(content))
	shortcodes := make([]Shortcode, 0)

	for i := 0; i < len(content); {
		next, kind := nextMarker(content, i)
		if next == -1 {
			out.WriteString(content[i:])
			break
		}

		out.WriteString(content[i:next])
		i = next

		switch kind {
		case "ignored-inline":
			end := strings.Index(content[i:], "*/}}")
			if end == -1 {
				return "", nil, fmt.Errorf("unterminated ignored inline shortcode")
			}
			raw := content[i : i+end+4]
			unignored := strings.Replace(raw, "{{/*", "{{", 1)
			unignored = strings.Replace(unignored, "*/}}", "}}", 1)
			out.WriteString(unignored)
			i += end + 4
		case "inline":
			end := strings.Index(content[i:], "}}")
			if end == -1 {
				return "", nil, fmt.Errorf("unterminated inline shortcode")
			}
			inner := strings.TrimSpace(content[i+2 : i+end])
			name, args, err := parseCall(inner)
			if err != nil {
				return "", nil, err
			}
			start := out.Len()
			out.WriteString(Placeholder)
			shortcodes = append(shortcodes, Shortcode{
				Name: name,
				Args: args,
				Span: [2]int{start, start + len(Placeholder)},
				Nth:  counter.Next(name),
			})
			i += end + 2
		case "ignored-body":
			startEnd := strings.Index(content[i:], "*/%}")
			if startEnd == -1 {
				return "", nil, fmt.Errorf("unterminated ignored body shortcode start")
			}
			openRaw := content[i : i+startEnd+4]
			closeIdx := strings.Index(content[i+startEnd+4:], "{%/* end */%}")
			if closeIdx == -1 {
				return "", nil, fmt.Errorf("unterminated ignored body shortcode")
			}
			bodyStart := i + startEnd + 4
			bodyEnd := bodyStart + closeIdx
			closeRaw := "{%/* end */%}"
			openUnignored := strings.Replace(openRaw, "{%/*", "{%", 1)
			openUnignored = strings.Replace(openUnignored, "*/%}", "%}", 1)
			closeUnignored := strings.Replace(closeRaw, "{%/*", "{%", 1)
			closeUnignored = strings.Replace(closeUnignored, "*/%}", "%}", 1)
			out.WriteString(openUnignored)
			out.WriteString(content[bodyStart:bodyEnd])
			out.WriteString(closeUnignored)
			i = bodyEnd + len(closeRaw)
		case "body":
			tagEnd := strings.Index(content[i:], "%}")
			if tagEnd == -1 {
				return "", nil, fmt.Errorf("unterminated shortcode body start")
			}
			startInner := strings.TrimSpace(content[i+2 : i+tagEnd])
			name, args, err := parseCall(startInner)
			if err != nil {
				return "", nil, err
			}
			nth := counter.Next(name)

			bodyOpenEnd := i + tagEnd + 2
			bodyTagStart, bodyTagEnd, err := findMatchingEnd(content, bodyOpenEnd)
			if err != nil {
				return "", nil, err
			}

			rawBody := strings.TrimSpace(content[bodyOpenEnd:bodyTagStart])
			parsedBody, innerSC, err := parseWithCounter(rawBody, counter)
			if err != nil {
				return "", nil, err
			}

			start := out.Len()
			out.WriteString(Placeholder)
			bodyCopy := parsedBody
			shortcodes = append(shortcodes, Shortcode{
				Name:  name,
				Args:  args,
				Span:  [2]int{start, start + len(Placeholder)},
				Body:  &bodyCopy,
				Nth:   nth,
				Inner: innerSC,
			})

			i = bodyTagEnd
		default:
			out.WriteByte(content[i])
			i++
		}
	}

	return out.String(), shortcodes, nil
}

func nextMarker(s string, from int) (int, string) {
	markers := []struct {
		needle string
		kind   string
	}{
		{"{{/*", "ignored-inline"},
		{"{{", "inline"},
		{"{%/*", "ignored-body"},
		{"{%", "body"},
	}

	best := -1
	bestKind := ""
	for _, m := range markers {
		idx := strings.Index(s[from:], m.needle)
		if idx == -1 {
			continue
		}
		abs := from + idx
		if best == -1 || abs < best {
			best = abs
			bestKind = m.kind
		}
	}

	return best, bestKind
}

func findMatchingEnd(content string, from int) (int, int, error) {
	depth := 1
	for i := from; i < len(content); {
		next := strings.Index(content[i:], "{%")
		if next == -1 {
			break
		}
		tagStart := i + next
		tagCloseRel := strings.Index(content[tagStart:], "%}")
		if tagCloseRel == -1 {
			return 0, 0, fmt.Errorf("unterminated shortcode body")
		}
		tagEnd := tagStart + tagCloseRel + 2
		inner := strings.TrimSpace(content[tagStart+2 : tagStart+tagCloseRel])

		if strings.HasPrefix(inner, "/*") {
			i = tagEnd
			continue
		}

		if inner == "end" {
			depth--
			if depth == 0 {
				return tagStart, tagEnd, nil
			}
		} else if looksLikeCall(inner) {
			depth++
		}

		i = tagEnd
	}

	return 0, 0, fmt.Errorf("shortcode body is missing {%% end %%}")
}

func looksLikeCall(s string) bool {
	open := strings.IndexByte(s, '(')
	close := strings.LastIndexByte(s, ')')
	return open > 0 && close == len(s)-1
}

func parseCall(s string) (string, map[string]any, error) {
	open := strings.IndexByte(s, '(')
	close := strings.LastIndexByte(s, ')')
	if open <= 0 || close != len(s)-1 {
		return "", nil, fmt.Errorf("invalid shortcode call: %q", s)
	}
	name := strings.TrimSpace(s[:open])
	argText := strings.TrimSpace(s[open+1 : close])
	args := map[string]any{}
	if argText == "" {
		return name, args, nil
	}

	parts, err := splitTopLevel(argText, ',')
	if err != nil {
		return "", nil, err
	}
	for _, p := range parts {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			return "", nil, fmt.Errorf("invalid argument: %q", p)
		}
		k := strings.TrimSpace(kv[0])
		v, err := parseLiteral(strings.TrimSpace(kv[1]))
		if err != nil {
			return "", nil, err
		}
		args[k] = v
	}

	return name, args, nil
}

func parseLiteral(s string) (any, error) {
	if s == "true" {
		return true, nil
	}
	if s == "false" {
		return false, nil
	}

	if len(s) >= 2 {
		q := s[0]
		if (q == '\'' || q == '"' || q == '`') && s[len(s)-1] == q {
			return s[1 : len(s)-1], nil
		}
	}

	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		inner := strings.TrimSpace(s[1 : len(s)-1])
		if inner == "" {
			return []any{}, nil
		}
		parts, err := splitTopLevel(inner, ',')
		if err != nil {
			return nil, err
		}
		arr := make([]any, 0, len(parts))
		for _, p := range parts {
			v, err := parseLiteral(strings.TrimSpace(p))
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		return arr, nil
	}

	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i, nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, nil
	}

	return nil, fmt.Errorf("unknown literal: %q", s)
}

func splitTopLevel(s string, sep rune) ([]string, error) {
	parts := make([]string, 0)
	start := 0
	depth := 0
	var quote rune
	escaped := false

	for i, r := range s {
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}

		switch r {
		case '\'', '"', '`':
			quote = r
		case '[':
			depth++
		case ']':
			if depth == 0 {
				return nil, fmt.Errorf("unbalanced brackets in %q", s)
			}
			depth--
		default:
			if r == sep && depth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}

	if quote != 0 {
		return nil, fmt.Errorf("unterminated string in %q", s)
	}
	if depth != 0 {
		return nil, fmt.Errorf("unbalanced brackets in %q", s)
	}

	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts, nil
}
