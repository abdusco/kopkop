package shortcode

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInsertMarkdownShortcodes_ReplacesMarkdownShortcodes(t *testing.T) {
	t.Parallel()

	content := Placeholder + Placeholder
	shortcodes := []Shortcode{
		{Name: "a", Span: [2]int{0, len(Placeholder)}, Nth: 1},
		{Name: "a", Span: [2]int{len(Placeholder), len(Placeholder) * 2}, Nth: 2},
	}

	out, htmlSC, err := InsertMarkdownShortcodes(
		content,
		shortcodes,
		func(sc Shortcode) (string, error) { return fmt.Sprintf("%d", sc.Nth), nil },
		func(sc Shortcode) bool { return true },
	)
	require.NoError(t, err)
	assert.Equal(t, "12", out)
	assert.Empty(t, htmlSC)
}

func TestInsertMarkdownShortcodes_LeavesHTMLShortcodes(t *testing.T) {
	t.Parallel()

	content := "Much wow " + Placeholder
	shortcodes := []Shortcode{
		{Name: "bodied", Span: [2]int{9, 9 + len(Placeholder)}, Nth: 1},
	}

	out, htmlSC, err := InsertMarkdownShortcodes(
		content,
		shortcodes,
		func(sc Shortcode) (string, error) { return "IGNORED", nil },
		func(sc Shortcode) bool { return false },
	)
	require.NoError(t, err)
	assert.Equal(t, content, out)
	require.Len(t, htmlSC, 1)
	assert.Equal(t, "bodied", htmlSC[0].Name)
}

func TestInsertMarkdownShortcodes_UpdatesRangesByPriorTransforms(t *testing.T) {
	t.Parallel()

	content := "x " + Placeholder + " " + Placeholder
	firstStart := 2
	firstEnd := 2 + len(Placeholder)
	secondStart := firstEnd + 1
	secondEnd := secondStart + len(Placeholder)

	shortcodes := []Shortcode{
		{Name: "first", Span: [2]int{firstStart, firstEnd}, Nth: 1},
		{Name: "second", Span: [2]int{secondStart, secondEnd}, Nth: 1},
	}

	out, _, err := InsertMarkdownShortcodes(
		content,
		shortcodes,
		func(sc Shortcode) (string, error) {
			if sc.Name == "first" {
				return "A", nil
			}
			return "BBBB", nil
		},
		func(sc Shortcode) bool { return true },
	)
	require.NoError(t, err)
	assert.Equal(t, "x A BBBB", out)
}
