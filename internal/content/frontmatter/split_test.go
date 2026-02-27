package frontmatter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testMeta struct {
	Title       string `toml:"title" yaml:"title"`
	Description string `toml:"description" yaml:"description"`
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "tests", "fixtures", "zola", "frontmatter", name)
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func TestParseFrontMatter_ValidFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fixture  string
		wantBody string
	}{
		{name: "toml with body", fixture: "toml_with_body.md", wantBody: "Hello\n"},
		{name: "yaml with body", fixture: "yaml_with_body.md", wantBody: "Hello\n"},
		{name: "toml only", fixture: "toml_only.md", wantBody: ""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content := readFixture(t, tc.fixture)
			meta, body, err := ParseFrontMatter[testMeta](tc.fixture, content)
			require.NoError(t, err)
			assert.Equal(t, "Title", meta.Title)
			assert.Equal(t, "hey there", meta.Description)
			assert.Equal(t, tc.wantBody, body)
		})
	}
}

func TestSplitContent_FrontMatterNotFound(t *testing.T) {
	t.Parallel()

	_, _, err := SplitContent("missing.md", "# no front matter")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "couldn't find front matter")
	assert.Contains(t, err.Error(), "missing.md")
}

func TestSplitContent_DelimiterLikeBodyPreserved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fixture  string
		wantBody string
	}{
		{name: "body with pluses", fixture: "toml_body_with_pluses.md", wantBody: "+++\n"},
		{name: "body with minuses", fixture: "toml_body_with_minuses.md", wantBody: "---\n"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			content := readFixture(t, tc.fixture)
			_, body, err := SplitContent(tc.fixture, content)
			require.NoError(t, err)
			assert.Equal(t, tc.wantBody, body)
		})
	}
}

func TestParseFrontMatter_ErrorIncludesPathContext(t *testing.T) {
	t.Parallel()

	content := readFixture(t, "invalid_toml.md")
	_, _, err := ParseFrontMatter[testMeta]("content/posts/invalid_toml.md", content)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content/posts/invalid_toml.md")
	assert.Contains(t, err.Error(), "toml deserialize error")
}
