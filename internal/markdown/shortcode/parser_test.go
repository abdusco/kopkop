package shortcode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		input        string
		assertResult func(t *testing.T, out string, shortcodes []Shortcode, err error)
	}{
		{
			name:  "extract inline shortcode with args",
			input: "Inline shortcode: {{ hello(string='hey', int=1, float=2.1, bool=true, array=[true, false]) }} hey",
			assertResult: func(t *testing.T, out string, shortcodes []Shortcode, err error) {
				require.NoError(t, err)
				assert.Equal(t, "Inline shortcode: "+Placeholder+" hey", out)
				require.Len(t, shortcodes, 1)

				sc := shortcodes[0]
				assert.Equal(t, "hello", sc.Name)
				assert.Equal(t, 1, sc.Nth)
				assert.Nil(t, sc.Body)
				assert.Equal(t, [2]int{18, 18 + len(Placeholder)}, sc.Span)
				assert.Equal(t, "hey", sc.Args["string"])
				assert.Equal(t, int64(1), sc.Args["int"])
				assert.Equal(t, 2.1, sc.Args["float"])
				assert.Equal(t, true, sc.Args["bool"])

				arr, ok := sc.Args["array"].([]any)
				require.True(t, ok)
				require.Len(t, arr, 2)
				assert.Equal(t, true, arr[0])
				assert.Equal(t, false, arr[1])
			},
		},
		{
			name:  "ignored inline shortcode is unignored",
			input: "Hello World {{/* youtube() */}} hey",
			assertResult: func(t *testing.T, out string, shortcodes []Shortcode, err error) {
				require.NoError(t, err)
				assert.Equal(t, "Hello World {{ youtube() }} hey", out)
				assert.Empty(t, shortcodes)
			},
		},
		{
			name:  "extract shortcode with body",
			input: "Body shortcode\n {% quote(author='Bobby', array=[[true]]) %}DROP TABLES;{% end %} \n hey",
			assertResult: func(t *testing.T, out string, shortcodes []Shortcode, err error) {
				require.NoError(t, err)
				assert.Equal(t, "Body shortcode\n "+Placeholder+" \n hey", out)
				require.Len(t, shortcodes, 1)

				sc := shortcodes[0]
				assert.Equal(t, "quote", sc.Name)
				assert.Equal(t, 1, sc.Nth)
				require.NotNil(t, sc.Body)
				assert.Equal(t, "DROP TABLES;", *sc.Body)
				assert.Empty(t, sc.Inner)
			},
		},
		{
			name:  "ignored body shortcode is unignored",
			input: "Hello World {%/* youtube() */%} Somebody {%/* end */%} hey",
			assertResult: func(t *testing.T, out string, shortcodes []Shortcode, err error) {
				require.NoError(t, err)
				assert.Equal(t, "Hello World {% youtube() %} Somebody {% end %} hey", out)
				assert.Empty(t, shortcodes)
			},
		},
		{
			name:  "multiple shortcodes increment nth",
			input: "Hello World {% youtube() %} Somebody {% end %} {{ hello() }}\n {{hello()}}",
			assertResult: func(t *testing.T, out string, shortcodes []Shortcode, err error) {
				require.NoError(t, err)
				assert.Equal(t, "Hello World "+Placeholder+" "+Placeholder+"\n "+Placeholder, out)
				require.Len(t, shortcodes, 3)
				assert.Equal(t, "youtube", shortcodes[0].Name)
				assert.Equal(t, 1, shortcodes[0].Nth)
				assert.Equal(t, "hello", shortcodes[1].Name)
				assert.Equal(t, 1, shortcodes[1].Nth)
				assert.Equal(t, "hello", shortcodes[2].Name)
				assert.Equal(t, 2, shortcodes[2].Nth)
			},
		},
		{
			name:  "nested shortcode bodies",
			input: "Hello World {% i_am_gonna_nest() %} Somebody {% i_am_gonna_nest() %} Somebody {% end %} {% end %}!!",
			assertResult: func(t *testing.T, out string, shortcodes []Shortcode, err error) {
				require.NoError(t, err)
				assert.Equal(t, "Hello World "+Placeholder+"!!", out)
				require.Len(t, shortcodes, 1)

				sc := shortcodes[0]
				assert.Equal(t, "i_am_gonna_nest", sc.Name)
				assert.Equal(t, 1, sc.Nth)
				require.NotNil(t, sc.Body)
				assert.Equal(t, "Somebody "+Placeholder, *sc.Body)
				require.Len(t, sc.Inner, 1)
				assert.Equal(t, "i_am_gonna_nest", sc.Inner[0].Name)
				assert.Equal(t, 2, sc.Inner[0].Nth)
			},
		},
		{
			name:  "non-call braces stay text",
			input: "use {{ .Title }} and {% if x %}y{% endif %} and {{ hello }}",
			assertResult: func(t *testing.T, out string, shortcodes []Shortcode, err error) {
				require.NoError(t, err)
				assert.Equal(t, "use {{ .Title }} and {% if x %}y{% endif %} and {{ hello }}", out)
				assert.Empty(t, shortcodes)
			},
		},
		{
			name:  "closing braces inside a string argument",
			input: `{{ code(t="a}}b") }}`,
			assertResult: func(t *testing.T, out string, shortcodes []Shortcode, err error) {
				require.NoError(t, err)
				assert.Equal(t, Placeholder, out)
				require.Len(t, shortcodes, 1)
				assert.Equal(t, "a}}b", shortcodes[0].Args["t"])
			},
		},
		{
			name:  "call with an invalid argument returns error",
			input: "bad {{ hello(a=b) }}",
			assertResult: func(t *testing.T, out string, shortcodes []Shortcode, err error) {
				require.Error(t, err)
				assert.Equal(t, "", out)
				assert.Nil(t, shortcodes)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, shortcodes, err := Parse(tc.input)
			tc.assertResult(t, out, shortcodes, err)
		})
	}
}

func TestShortcodeUpdateRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		initial      [2]int
		transforms   [][3]int
		assertResult func(t *testing.T, got [2]int)
	}{
		{
			name:    "expands when rendered output is longer",
			initial: [2]int{10, 20},
			transforms: [][3]int{
				{2, 8, 10},
			},
			assertResult: func(t *testing.T, got [2]int) {
				assert.Equal(t, [2]int{14, 24}, got)
			},
		},
		{
			name:    "ignores transform after shortcode",
			initial: [2]int{10, 20},
			transforms: [][3]int{
				{25, 30, 30},
			},
			assertResult: func(t *testing.T, got [2]int) {
				assert.Equal(t, [2]int{10, 20}, got)
			},
		},
		{
			name:    "applies multiple transforms in sequence",
			initial: [2]int{10, 20},
			transforms: [][3]int{
				{2, 8, 10},
				{5, 11, 10},
			},
			assertResult: func(t *testing.T, got [2]int) {
				assert.Equal(t, [2]int{18, 28}, got)
			},
		},
		{
			name:    "regression shrinks when rendered output is shorter",
			initial: [2]int{42, 65},
			transforms: [][3]int{
				{9, 32, 3},
			},
			assertResult: func(t *testing.T, got [2]int) {
				assert.Equal(t, [2]int{22, 45}, got)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sc := Shortcode{Span: tc.initial}
			for _, tr := range tc.transforms {
				sc.UpdateRange([2]int{tr[0], tr[1]}, tr[2])
			}

			tc.assertResult(t, sc.Span)
		})
	}
}
