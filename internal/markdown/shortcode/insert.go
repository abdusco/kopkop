package shortcode

import "fmt"

// InsertMarkdownShortcodes renders markdown shortcodes into content and keeps html shortcodes for later.
func InsertMarkdownShortcodes(
	content string,
	shortcodes []Shortcode,
	render func(Shortcode) (string, error),
	isMarkdown func(Shortcode) bool,
) (string, []Shortcode, error) {
	transforms := make([][3]int, 0) // start, end, rendered length
	htmlShortcodes := make([]Shortcode, 0)

	for i := range shortcodes {
		sc := shortcodes[i]
		for _, tr := range transforms {
			sc.UpdateRange([2]int{tr[0], tr[1]}, tr[2])
		}

		if !isMarkdown(sc) {
			htmlShortcodes = append(htmlShortcodes, sc)
			continue
		}

		res, err := render(sc)
		if err != nil {
			return "", nil, fmt.Errorf("render shortcode %q: %w", sc.Name, err)
		}

		start, end := sc.Span[0], sc.Span[1]
		content = content[:start] + res + content[end:]
		transforms = append(transforms, [3]int{start, end, len(res)})
	}

	return content, htmlShortcodes, nil
}
