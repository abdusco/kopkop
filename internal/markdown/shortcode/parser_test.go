package shortcode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		input           string
		wantOut         string
		wantCount       int
		wantFirstName   string
		wantFirstNth    int
		wantInnerCount  int
		wantIgnoredOnly bool
	}{
		{
			name:          "extract inline shortcode with args",
			input:         "Inline shortcode: {{ hello(string='hey', int=1, float=2.1, bool=true, array=[true, false]) }} hey",
			wantOut:       "Inline shortcode: " + Placeholder + " hey",
			wantCount:     1,
			wantFirstName: "hello",
			wantFirstNth:  1,
		},
		{
			name:            "ignored inline shortcode is unignored",
			input:           "Hello World {{/* youtube() */}} hey",
			wantOut:         "Hello World {{ youtube() }} hey",
			wantCount:       0,
			wantIgnoredOnly: true,
		},
		{
			name:          "extract shortcode with body",
			input:         "Body shortcode\n {% quote(author='Bobby', array=[[true]]) %}DROP TABLES;{% end %} \n hey",
			wantOut:       "Body shortcode\n " + Placeholder + " \n hey",
			wantCount:     1,
			wantFirstName: "quote",
			wantFirstNth:  1,
		},
		{
			name:            "ignored body shortcode is unignored",
			input:           "Hello World {%/* youtube() */%} Somebody {%/* end */%} hey",
			wantOut:         "Hello World {% youtube() %} Somebody {% end %} hey",
			wantCount:       0,
			wantIgnoredOnly: true,
		},
		{
			name:      "multiple shortcodes increment nth",
			input:     "Hello World {% youtube() %} Somebody {% end %} {{ hello() }}\n {{hello()}}",
			wantOut:   "Hello World " + Placeholder + " " + Placeholder + "\n " + Placeholder,
			wantCount: 3,
		},
		{
			name:           "nested shortcode bodies",
			input:          "Hello World {% i_am_gonna_nest() %} Somebody {% i_am_gonna_nest() %} Somebody {% end %} {% end %}!!",
			wantOut:        "Hello World " + Placeholder + "!!",
			wantCount:      1,
			wantFirstName:  "i_am_gonna_nest",
			wantFirstNth:   1,
			wantInnerCount: 1,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, shortcodes, err := Parse(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.wantOut, out)
			assert.Len(t, shortcodes, tc.wantCount)

			if tc.wantCount > 0 && !tc.wantIgnoredOnly {
				if tc.wantFirstName != "" {
					assert.Equal(t, tc.wantFirstName, shortcodes[0].Name)
				}
				if tc.wantFirstNth != 0 {
					assert.Equal(t, tc.wantFirstNth, shortcodes[0].Nth)
				}
				if tc.wantInnerCount != 0 {
					assert.Len(t, shortcodes[0].Inner, tc.wantInnerCount)
					if tc.name == "nested shortcode bodies" {
						assert.Equal(t, 2, shortcodes[0].Inner[0].Nth)
					}
				}
			}
		})
	}
}

func TestParse_InlineShortcodeArgTypes(t *testing.T) {
	t.Parallel()

	_, shortcodes, err := Parse("{{ hello(string='hey', int=1, float=2.1, bool=true, array=[true, false]) }}")
	require.NoError(t, err)
	require.Len(t, shortcodes, 1)

	args := shortcodes[0].Args
	assert.Equal(t, "hey", args["string"])
	assert.Equal(t, int64(1), args["int"])
	assert.Equal(t, 2.1, args["float"])
	assert.Equal(t, true, args["bool"])

	arr, ok := args["array"].([]any)
	require.True(t, ok)
	require.Len(t, arr, 2)
	assert.Equal(t, true, arr[0])
	assert.Equal(t, false, arr[1])
}

func TestParse_ShortcodeWithBodyContent(t *testing.T) {
	t.Parallel()

	_, shortcodes, err := Parse("{% quote(author='Bobby') %}DROP TABLES;{% end %}")
	require.NoError(t, err)
	require.Len(t, shortcodes, 1)
	require.NotNil(t, shortcodes[0].Body)
	assert.Equal(t, "DROP TABLES;", *shortcodes[0].Body)
}

func TestParse_MultipleNthValues(t *testing.T) {
	t.Parallel()

	_, shortcodes, err := Parse("{% youtube() %} a {% end %} {{ hello() }} {{hello()}}")
	require.NoError(t, err)
	require.Len(t, shortcodes, 3)

	assert.Equal(t, 1, shortcodes[0].Nth)
	assert.Equal(t, 1, shortcodes[1].Nth)
	assert.Equal(t, 2, shortcodes[2].Nth)
}
