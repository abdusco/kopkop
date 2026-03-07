package shortcode

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInsertMarkdownShortcodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		content      string
		shortcodes   []Shortcode
		render       func(Shortcode) (string, error)
		isMarkdown   func(Shortcode) bool
		assertResult func(t *testing.T, out string, htmlSC []Shortcode, err error)
	}{
		{
			name:    "replaces markdown shortcodes",
			content: Placeholder + Placeholder,
			shortcodes: []Shortcode{
				{Name: "a", Span: [2]int{0, len(Placeholder)}, Nth: 1},
				{Name: "a", Span: [2]int{len(Placeholder), len(Placeholder) * 2}, Nth: 2},
			},
			render:     func(sc Shortcode) (string, error) { return fmt.Sprintf("%d", sc.Nth), nil },
			isMarkdown: func(sc Shortcode) bool { return true },
			assertResult: func(t *testing.T, out string, htmlSC []Shortcode, err error) {
				require.NoError(t, err)
				assert.Equal(t, "12", out)
				assert.Empty(t, htmlSC)
			},
		},
		{
			name:    "leaves html shortcodes",
			content: "Much wow " + Placeholder,
			shortcodes: []Shortcode{
				{Name: "bodied", Span: [2]int{9, 9 + len(Placeholder)}, Nth: 1},
			},
			render:     func(sc Shortcode) (string, error) { return "IGNORED", nil },
			isMarkdown: func(sc Shortcode) bool { return false },
			assertResult: func(t *testing.T, out string, htmlSC []Shortcode, err error) {
				require.NoError(t, err)
				assert.Equal(t, "Much wow "+Placeholder, out)
				require.Len(t, htmlSC, 1)
				assert.Equal(t, "bodied", htmlSC[0].Name)
				assert.Equal(t, 1, htmlSC[0].Nth)
			},
		},
		{
			name: "updates ranges by prior transforms",
			content: func() string {
				return "x " + Placeholder + " " + Placeholder
			}(),
			shortcodes: func() []Shortcode {
				firstStart := 2
				firstEnd := firstStart + len(Placeholder)
				secondStart := firstEnd + 1
				secondEnd := secondStart + len(Placeholder)
				return []Shortcode{
					{Name: "first", Span: [2]int{firstStart, firstEnd}, Nth: 1},
					{Name: "second", Span: [2]int{secondStart, secondEnd}, Nth: 1},
				}
			}(),
			render: func(sc Shortcode) (string, error) {
				if sc.Name == "first" {
					return "A", nil
				}
				return "BBBB", nil
			},
			isMarkdown: func(sc Shortcode) bool { return true },
			assertResult: func(t *testing.T, out string, htmlSC []Shortcode, err error) {
				require.NoError(t, err)
				assert.Equal(t, "x A BBBB", out)
				assert.Empty(t, htmlSC)
			},
		},
		{
			name:    "render errors include shortcode name",
			content: Placeholder,
			shortcodes: []Shortcode{
				{Name: "broken", Span: [2]int{0, len(Placeholder)}, Nth: 1},
			},
			render:     func(sc Shortcode) (string, error) { return "", errors.New("boom") },
			isMarkdown: func(sc Shortcode) bool { return true },
			assertResult: func(t *testing.T, out string, htmlSC []Shortcode, err error) {
				require.Error(t, err)
				assert.ErrorContains(t, err, "render shortcode \"broken\"")
				assert.ErrorContains(t, err, "boom")
				assert.Equal(t, "", out)
				assert.Nil(t, htmlSC)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, htmlSC, err := InsertMarkdownShortcodes(tc.content, tc.shortcodes, tc.render, tc.isMarkdown)
			tc.assertResult(t, out, htmlSC, err)
		})
	}
}
