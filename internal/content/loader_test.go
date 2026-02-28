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
