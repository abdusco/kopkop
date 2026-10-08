package content

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdusco/kopkop/internal/config"
)

func TestLoadLibrary_BuildsPagesSectionsTaxonomies(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content", "blog"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "blog", "_index.md"), []byte("+++\ntitle='Blog'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "blog", "post.md"), []byte("+++\ntitle='Post'\ntaxonomies={ tags=['go','kopkop'] }\n+++\nHello"), 0o644))

	cfg := config.Default()
	cfg.BaseURL = "https://example.com"
	cfg.Taxonomies = []config.TaxonomyConfig{{Name: "tags"}}

	lib, err := LoadLibrary(root, cfg, LoadOptions{})
	require.NoError(t, err)

	require.Len(t, lib.Sections, 2)
	require.Len(t, lib.Pages, 1)
	p := lib.Pages["blog/post.md"]
	require.NotNil(t, p)
	assert.Equal(t, "/blog/post/", p.Path)
	assert.Equal(t, "https://example.com/blog/post/", p.Permalink)

	tax := lib.Taxonomies["tags"]
	require.NotNil(t, tax)
	require.NotNil(t, tax.Terms["go"])
	require.NotNil(t, tax.Terms["kopkop"])
	assert.Equal(t, []string{"blog/post.md"}, tax.Terms["go"].Pages)
}

func TestLoadLibrary_SkipsHiddenFilesAndDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for name, body := range map[string]string{
		"content/_index.md":            "+++\ntitle='Home'\n+++\n",
		"content/post.md":              "Hello",
		"content/.hidden.md":           "Hidden file",
		"content/.obsidian/note.md":    "Hidden dir",
		"content/blog/.drafts/deep.md": "Nested hidden dir",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
	}

	cfg := config.Default()
	cfg.BaseURL = "https://example.com"
	lib, err := LoadLibrary(root, cfg, LoadOptions{})
	require.NoError(t, err)
	require.Len(t, lib.Pages, 1)
	assert.Contains(t, lib.Pages, "post.md")
}

func TestLoadLibrary_SortByWeightPutsUnweightedLast(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for name, body := range map[string]string{
		"content/blog/_index.md": "+++\nsort_by='weight'\n+++\n",
		"content/blog/a.md":      "+++\ntitle='none'\n+++\n",
		"content/blog/b.md":      "+++\nweight=5\n+++\n",
		"content/blog/c.md":      "+++\nweight=1\n+++\n",
		"content/blog/d.md":      "+++\nweight=0\n+++\n",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
	}

	cfg := config.Default()
	cfg.BaseURL = "https://example.com"
	lib, err := LoadLibrary(root, cfg, LoadOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"blog/d.md", "blog/c.md", "blog/b.md", "blog/a.md"}, lib.Sections["blog/_index.md"].Pages)
}

func TestShouldIgnoreContent(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		rel      string
		patterns []string
		want     bool
	}{
		{"no patterns", "a/b.md", nil, false},
		{"hidden file", "a/.b.md", nil, true},
		{"star crosses directories", "a/b/c.psd", []string{"*.psd"}, true},
		{"star with directory", "posts/ignored.md", []string{"*/ignored.md"}, true},
		{"double star prefix", "x/y/z.md", []string{"**/z.md"}, true},
		{"double star prefix matches root", "z.md", []string{"**/z.md"}, true},
		{"double star suffix", "drafts/a/b.md", []string{"drafts/**"}, true},
		{"alternation", "a.tmp", []string{"*.{tmp,bak}"}, true},
		{"no match", "a/b.md", []string{"*.psd", "c/*"}, false},
		{"dot is literal", "axpsd", []string{"*.psd"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, shouldIgnoreContent(tc.rel, tc.patterns))
		})
	}
}

func TestLoadLibrary_SkipsDraftsByDefault(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "draft.md"), []byte("+++\ntitle='Draft'\ndraft=true\n+++\nHidden"), 0o644))

	cfg := config.Default()
	cfg.BaseURL = "https://example.com"

	lib, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: false})
	require.NoError(t, err)
	assert.Len(t, lib.Pages, 0)

	lib2, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: true})
	require.NoError(t, err)
	assert.Len(t, lib2.Pages, 1)
}

func TestLoadLibrary_IgnoresHiddenAndConfiguredPatterns(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", ".hidden.md"), []byte("+++\ntitle='Hidden'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "ignore-me.md"), []byte("+++\ntitle='Ignore'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "keep-me.md"), []byte("+++\ntitle='Keep'\n+++\n"), 0o644))

	cfg := config.Default()
	cfg.BaseURL = "https://example.com"
	cfg.IgnoredContent = []string{"ignore-me.md"}

	lib, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: false})
	require.NoError(t, err)
	_, hasHidden := lib.Pages[".hidden.md"]
	_, hasIgnored := lib.Pages["ignore-me.md"]
	_, hasKept := lib.Pages["keep-me.md"]
	assert.False(t, hasHidden)
	assert.False(t, hasIgnored)
	assert.True(t, hasKept)
}

func TestLoadLibrary_AllowsMarkdownWithoutFrontMatter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("Home without front matter"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "plain.md"), []byte("Plain body"), 0o644))

	cfg := config.Default()
	cfg.BaseURL = "https://example.com"

	lib, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: false})
	require.NoError(t, err)
	require.Contains(t, lib.Sections, "_index.md")
	require.Contains(t, lib.Pages, "plain.md")
	assert.Equal(t, "Plain body", lib.Pages["plain.md"].RawContent)
}

func TestLoadLibrary_ColocatedIndexPageKeepsSectionPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content", "posts", "with-assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "posts", "_index.md"), []byte("+++\ntitle='Posts'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "posts", "with-assets", "index.md"), []byte("+++\ntitle='With Assets'\n+++\nHello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "posts", "with-assets", "with.js"), []byte("console.log('x')"), 0o644))

	cfg := config.Default()
	cfg.BaseURL = "https://example.com"

	lib, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: false})
	require.NoError(t, err)

	pg := lib.Pages["posts/with-assets/index.md"]
	require.NotNil(t, pg)
	assert.Equal(t, "/posts/with-assets/", pg.Path)
	assert.Equal(t, "https://example.com/posts/with-assets/", pg.Permalink)
	require.NotEmpty(t, pg.Assets)
}

func TestLoadLibrary_DraftSectionHidesChildPages(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content", "secret"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "secret", "_index.md"), []byte("+++\ntitle='Secret'\ndraft=true\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "secret", "page.md"), []byte("+++\ntitle='Page'\n+++\nhello"), 0o644))

	cfg := config.Default()
	cfg.BaseURL = "https://example.com"

	lib, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: false})
	require.NoError(t, err)
	require.NotContains(t, lib.Sections, "secret/_index.md")
	require.NotContains(t, lib.Pages, "secret/page.md")
}

func TestLoadLibrary_ParsesFrontMatterWithUTF8BOM(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "hello.md"), []byte("\ufeff+++\ntitle='Hello'\n+++\nBody"), 0o644))

	cfg := config.Default()
	cfg.BaseURL = "https://example.com"

	lib, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: false})
	require.NoError(t, err)
	pg := lib.Pages["hello.md"]
	require.NotNil(t, pg)
	assert.Equal(t, "Hello", pg.Meta.Title)
	assert.Equal(t, "Body", pg.RawContent)
}

func TestLoadLibrary_AttachesNestedIndexPageToNearestAncestorSection(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content", "2018"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "2018", "index.md"), []byte("+++\ntitle='Year index'\n+++\n"), 0o644))

	cfg := config.Default()
	cfg.BaseURL = "https://example.com"

	lib, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: false})
	require.NoError(t, err)

	rootSection := lib.Sections["_index.md"]
	require.NotNil(t, rootSection)
	assert.Contains(t, rootSection.Pages, "2018/index.md")
}
