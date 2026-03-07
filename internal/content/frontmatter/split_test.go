package frontmatter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testMeta struct {
	Title       string `toml:"title" yaml:"title"`
	Description string `toml:"description" yaml:"description"`
}

func TestParseFrontMatter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		filePath     string
		content      string
		assertResult func(t *testing.T, gotMeta testMeta, gotBody string, err error)
	}{
		{
			name:     "toml with body",
			filePath: "toml_with_body.md",
			content: `+++
title = "Title"
description = "hey there"
date = 2002-10-12
+++
Hello
`,
			assertResult: func(t *testing.T, gotMeta testMeta, gotBody string, err error) {
				require.NoError(t, err)
				assert.Equal(t, testMeta{Title: "Title", Description: "hey there"}, gotMeta)
				assert.Equal(t, "Hello\n", gotBody)
			},
		},
		{
			name:     "yaml with body",
			filePath: "yaml_with_body.md",
			content: `---
title: Title
description: hey there
date: 2002-10-12
---
Hello
`,
			assertResult: func(t *testing.T, gotMeta testMeta, gotBody string, err error) {
				require.NoError(t, err)
				assert.Equal(t, testMeta{Title: "Title", Description: "hey there"}, gotMeta)
				assert.Equal(t, "Hello\n", gotBody)
			},
		},
		{
			name:     "toml only",
			filePath: "toml_only.md",
			content: `+++
title = "Title"
description = "hey there"
date = 2002-10-12
+++
`,
			assertResult: func(t *testing.T, gotMeta testMeta, gotBody string, err error) {
				require.NoError(t, err)
				assert.Equal(t, testMeta{Title: "Title", Description: "hey there"}, gotMeta)
				assert.Equal(t, "", gotBody)
			},
		},
		{
			name:     "invalid toml includes path context",
			filePath: "content/posts/invalid_toml.md",
			content: `+++
title = "Title"
description = hey there
+++
Hello
`,
			assertResult: func(t *testing.T, gotMeta testMeta, gotBody string, err error) {
				require.Error(t, err)
				assert.ErrorContains(t, err, "error parsing front matter for \"content/posts/invalid_toml.md\"")
				assert.ErrorContains(t, err, "toml deserialize error")
				assert.Equal(t, "", gotBody)
				assert.Equal(t, testMeta{}, gotMeta)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			meta, body, err := ParseFrontMatter[testMeta](tc.filePath, tc.content)
			tc.assertResult(t, meta, body, err)
		})
	}
}

func TestSplitContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		content      string
		assertResult func(t *testing.T, gotRaw RawFrontMatter, gotBody string, err error)
	}{
		{
			name: "toml with body",
			content: `+++
title = "Title"
description = "hey there"
+++
Hello
`,
			assertResult: func(t *testing.T, gotRaw RawFrontMatter, gotBody string, err error) {
				require.NoError(t, err)
				assert.Equal(t, RawFrontMatter{
					Format: FormatTOML,
					Data:   "title = \"Title\"\ndescription = \"hey there\"",
				}, gotRaw)
				assert.Equal(t, "Hello\n", gotBody)
			},
		},
		{
			name: "yaml with body",
			content: `---
title: Title
description: hey there
---
Hello
`,
			assertResult: func(t *testing.T, gotRaw RawFrontMatter, gotBody string, err error) {
				require.NoError(t, err)
				assert.Equal(t, RawFrontMatter{
					Format: FormatYAML,
					Data:   "title: Title\ndescription: hey there",
				}, gotRaw)
				assert.Equal(t, "Hello\n", gotBody)
			},
		},
		{
			name: "toml only",
			content: `+++
title = "Title"
description = "hey there"
+++
`,
			assertResult: func(t *testing.T, gotRaw RawFrontMatter, gotBody string, err error) {
				require.NoError(t, err)
				assert.Equal(t, RawFrontMatter{
					Format: FormatTOML,
					Data:   "title = \"Title\"\ndescription = \"hey there\"",
				}, gotRaw)
				assert.Equal(t, "", gotBody)
			},
		},
		{
			name: "body with pluses",
			content: `+++
title = "Title"
description = "hey there"
+++
+++
`,
			assertResult: func(t *testing.T, gotRaw RawFrontMatter, gotBody string, err error) {
				require.NoError(t, err)
				assert.Equal(t, RawFrontMatter{
					Format: FormatTOML,
					Data:   "title = \"Title\"\ndescription = \"hey there\"",
				}, gotRaw)
				assert.Equal(t, "+++\n", gotBody)
			},
		},
		{
			name: "body with minuses",
			content: `+++
title = "Title"
description = "hey there"
+++
---
`,
			assertResult: func(t *testing.T, gotRaw RawFrontMatter, gotBody string, err error) {
				require.NoError(t, err)
				assert.Equal(t, RawFrontMatter{
					Format: FormatTOML,
					Data:   "title = \"Title\"\ndescription = \"hey there\"",
				}, gotRaw)
				assert.Equal(t, "---\n", gotBody)
			},
		},
		{
			name: "leading whitespace before delimiter",
			content: `
	
  +++
foo = "bar"
+++
Body
`,
			assertResult: func(t *testing.T, gotRaw RawFrontMatter, gotBody string, err error) {
				require.NoError(t, err)
				assert.Equal(t, RawFrontMatter{Format: FormatTOML, Data: "foo = \"bar\""}, gotRaw)
				assert.Equal(t, "Body\n", gotBody)
			},
		},
		{
			name:    "front matter not found",
			content: "# no front matter",
			assertResult: func(t *testing.T, gotRaw RawFrontMatter, gotBody string, err error) {
				require.Error(t, err)
				assert.ErrorContains(t, err, "couldn't find front matter")
				assert.Equal(t, RawFrontMatter{}, gotRaw)
				assert.Equal(t, "", gotBody)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			raw, body, err := SplitContent(tc.content)
			tc.assertResult(t, raw, body, err)
		})
	}
}
