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
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "blog", "post.md"), []byte("+++\ntitle='Post'\ntaxonomies={ tags=['go','zola'] }\n+++\nHello"), 0o644))

	cfg := config.Default()
	cfg.BaseURL = "https://example.com"
	cfg.Taxonomies = []config.TaxonomyConfig{{Name: "tags"}}

	lib, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: false, RenderMarkdown: true})
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
	require.NotNil(t, tax.Terms["zola"])
	assert.Equal(t, []string{"blog/post.md"}, tax.Terms["go"].Pages)
}

func TestLoadLibrary_SkipsDraftsByDefault(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "draft.md"), []byte("+++\ntitle='Draft'\ndraft=true\n+++\nHidden"), 0o644))

	cfg := config.Default()
	cfg.BaseURL = "https://example.com"

	lib, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: false, RenderMarkdown: false})
	require.NoError(t, err)
	assert.Len(t, lib.Pages, 0)

	lib2, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: true, RenderMarkdown: false})
	require.NoError(t, err)
	assert.Len(t, lib2.Pages, 1)
}

func TestLoadLibrary_MultilingualPagePathsAndParentSection(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content", "blog"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.fr.md"), []byte("+++\ntitle='Accueil'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "blog", "_index.md"), []byte("+++\ntitle='Blog'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "blog", "_index.fr.md"), []byte("+++\ntitle='Blog FR'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "blog", "post.fr.md"), []byte("+++\ntitle='Post FR'\n+++\nBonjour"), 0o644))

	cfg := config.Default()
	cfg.BaseURL = "https://example.com"
	cfg.DefaultLanguage = "en"
	cfg.Languages = map[string]config.LanguageOptions{"fr": {Title: "Francais"}}

	lib, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: false, RenderMarkdown: false})
	require.NoError(t, err)

	pg := lib.Pages["blog/post.fr.md"]
	require.NotNil(t, pg)
	assert.Equal(t, "fr", pg.Lang)
	assert.Equal(t, "/fr/blog/post/", pg.Path)
	assert.Equal(t, "blog/_index.fr.md", pg.ParentSection)

	sec := lib.Sections["blog/_index.fr.md"]
	require.NotNil(t, sec)
	assert.Contains(t, sec.Pages, "blog/post.fr.md")
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

	lib, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: false, RenderMarkdown: false})
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

	lib, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: false, RenderMarkdown: false})
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

	lib, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: false, RenderMarkdown: false})
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

	lib, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: false, RenderMarkdown: false})
	require.NoError(t, err)
	require.NotContains(t, lib.Sections, "secret/_index.md")
	require.NotContains(t, lib.Pages, "secret/page.md")
}

func TestLoadLibrary_SectionFrenchPathHasSlash(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content", "blog"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "blog", "_index.fr.md"), []byte("+++\ntitle='Blog FR'\n+++\n"), 0o644))

	cfg := config.Default()
	cfg.BaseURL = "https://example.com"
	cfg.DefaultLanguage = "en"
	cfg.Languages = map[string]config.LanguageOptions{"fr": {Title: "Francais"}}

	lib, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: false, RenderMarkdown: false})
	require.NoError(t, err)
	sec := lib.Sections["blog/_index.fr.md"]
	require.NotNil(t, sec)
	assert.Equal(t, "/fr/blog/", sec.Path)
}

func TestLoadLibrary_ColocatedAssetsDetectedForTranslatedIndex(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content", "blog", "with-assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "blog", "_index.md"), []byte("+++\ntitle='Blog'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "blog", "with-assets", "index.fr.md"), []byte("+++\ntitle='FR'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "blog", "with-assets", "some.js"), []byte("console.log('x')"), 0o644))

	cfg := config.Default()
	cfg.BaseURL = "https://example.com"
	cfg.DefaultLanguage = "en"
	cfg.Languages = map[string]config.LanguageOptions{"fr": {Title: "Francais"}}

	lib, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: false, RenderMarkdown: false})
	require.NoError(t, err)

	pg := lib.Pages["blog/with-assets/index.fr.md"]
	require.NotNil(t, pg)
	require.Len(t, pg.Assets, 1)
	assert.Equal(t, "/fr/blog/with-assets/", pg.Path)
}

func TestLoadLibrary_ParsesFrontMatterWithUTF8BOM(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\n+++\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "hello.md"), []byte("\ufeff+++\ntitle='Hello'\n+++\nBody"), 0o644))

	cfg := config.Default()
	cfg.BaseURL = "https://example.com"

	lib, err := LoadLibrary(root, cfg, LoadOptions{IncludeDrafts: false, RenderMarkdown: false})
	require.NoError(t, err)
	pg := lib.Pages["hello.md"]
	require.NotNil(t, pg)
	assert.Equal(t, "Hello", pg.Meta.Title)
	assert.Equal(t, "Body", pg.RawContent)
}
