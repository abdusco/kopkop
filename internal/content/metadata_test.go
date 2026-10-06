package content

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abdusco/kopkop/internal/config"
	"github.com/stretchr/testify/require"
)

func TestContentSectionNaming(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content"), 0o755))
	for _, name := range []string{"_index.md", "_index_notes.md", "_index.fr.md", "你好.md", "Crème Brûlée.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "content", name), []byte("Body"), 0o644))
	}
	lib, err := LoadLibrary(root, config.Default(), LoadOptions{})
	require.NoError(t, err)
	require.Len(t, lib.Sections, 1)
	require.Contains(t, lib.Sections, "_index.md")
	require.Len(t, lib.Pages, 4)
	require.Contains(t, lib.Pages, "_index_notes.md")
	require.Contains(t, lib.Pages, "_index.fr.md")
	require.Equal(t, "/你好/", lib.Pages["你好.md"].Path)
	require.Equal(t, "/crème-brûlée/", lib.Pages["Crème Brûlée.md"].Path)
}

func TestBundleSlugs(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, wantPath, wantSlug string
	}{
		{"default bundle", "posts/bundle/index.md", "Body", "/posts/bundle/", ""},
		{"override bundle", "posts/bundle/index.md", "+++\nslug='新名字'\n+++\nBody", "/posts/新名字/", "新名字"},
		{"root index", "index.md", "Body", "/", ""},
		{"override root index", "index.md", "+++\nslug='Accueil'\n+++\nBody", "/accueil/", "accueil"},
		{"path wins", "posts/bundle/index.md", "+++\nslug='Custom'\npath='elsewhere'\n+++\nBody", "/elsewhere/", "custom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			abs := filepath.Join(root, filepath.FromSlash(tc.path))
			require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(abs), "photo.png"), []byte("asset"), 0o644))
			page, err := parsePage(abs, tc.path, tc.body, config.Default())
			require.NoError(t, err)
			require.Equal(t, tc.wantPath, page.Path)
			require.Equal(t, tc.wantSlug, page.Slug)
			require.Equal(t, config.Default().MakePermalink(tc.wantPath), page.Permalink)
		})
	}
}

func TestInvalidPageMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, wantError string
	}{
		{"punctuation filename", "!!!.md", "Body", "filename normalizes to an empty slug"},
		{"empty explicit slug", "post.md", "+++\nslug=''\n+++\nBody", "post.md:2: invalid slug"},
		{"symbols slug", "bundle/index.md", "+++\nslug='🔥'\n+++\nBody", "bundle/index.md:2: invalid slug"},
		{"invalid date", "post.md", "+++\ntitle='Title'\ndate='2024-02-30'\n+++\nBody", "post.md:3: invalid date"},
		{"invalid updated", "post.md", "---\nupdated: yesterday\n---\nBody", "post.md:2: invalid updated"},
		{"null date", "post.md", "---\ndate: null\n---\nBody", "post.md:2: invalid date"},
		{"empty date", "post.md", "+++\ndate=''\n+++\nBody", "post.md:2: invalid date"},
		{"numeric date", "post.md", "+++\ndate=123\n+++\nBody", "post.md:2: invalid date"},
		{"invalid filename date", "2024-02-30-post.md", "Body", "invalid filename date"},
		{"source line with BOM and whitespace", "post.md", "\ufeff\n\n+++\ntitle='Title'\ndate='bad'\n+++\nBody", "post.md:5: invalid date"},
		{"empty taxonomy term", "post.md", "+++\n[taxonomies]\ntags=['🔥']\n+++\nBody", "post.md:2: invalid taxonomies"},
		{"empty taxonomy name", "post.md", "---\ntaxonomies:\n  '🔥': [term]\n---\nBody", "post.md:2: invalid taxonomies"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parsePage(filepath.Join(t.TempDir(), tc.path), tc.path, tc.body, config.Default())
			require.ErrorContains(t, err, tc.wantError)
		})
	}
}

func TestValidPageDates(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, wantDate string
	}{
		{"TOML date", "post.md", "+++\ndate=2024-02-29\n+++\nBody", "2024-02-29T00:00:00Z"},
		{"YAML date", "post.md", "---\ndate: 2024-02-29\n---\nBody", "2024-02-29T00:00:00Z"},
		{"offset midnight", "post.md", "+++\ndate=2024-01-01T00:00:00+02:00\n+++\nBody", "2023-12-31T22:00:00Z"},
		{"filename date with slug override", "2024-02-29-post.md", "+++\nslug='different'\n+++\nBody", "2024-02-29T00:00:00Z"},
		{"explicit slug rescues punctuation filename", "!!!.md", "+++\nslug='Valid'\ndate='2024-02-29'\n+++\nBody", "2024-02-29T00:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := parsePage(filepath.Join(t.TempDir(), tc.path), tc.path, tc.body, config.Default())
			require.NoError(t, err)
			require.NotNil(t, page.Date)
			require.Equal(t, tc.wantDate, page.Date.Format(time.RFC3339))
		})
	}
}

func TestNegativeSectionPagination(t *testing.T) {
	_, err := parseSection("_index.md", "_index.md", "+++\npaginate_by=-1\n+++\nBody", config.Default())
	require.ErrorContains(t, err, "_index.md:2: invalid paginate_by")
}
