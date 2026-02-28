package site

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSiteBuild_Minimal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "zola.toml"), []byte(`
base_url = "https://example.com"
title = "Demo"
output_dir = "public"
compile_sass = false
generate_sitemap = true
generate_feeds = false
build_search_index = false
generate_robots_txt = true
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\n+++\nWelcome"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "post.md"), []byte("+++\ntitle='Post'\n+++\nHello world"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "page.html"), []byte("<html><body>{{ page.content|safe }}</body></html>"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "section.html"), []byte("<html><body>{{ section.title }}</body></html>"), 0o644))

	s, err := New(root, filepath.Join(root, "zola.toml"))
	require.NoError(t, err)
	require.NoError(t, s.Load(false))
	require.NoError(t, s.Build(BuildOptions{BuildMode: BuildDisk, Force: true}))

	_, err = os.Stat(filepath.Join(root, "public", "post", "index.html"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "public", "sitemap.xml"))
	require.NoError(t, err)
}

func TestMinifyHTML(t *testing.T) {
	t.Parallel()

	in := "<html>\n  <body>  <h1> Hi </h1> </body>\n</html>"
	out := minifyHTML(in)
	require.Equal(t, "<html><body><h1> Hi </h1></body></html>", out)
}

func TestSiteBuild_ConcurrentDeterministicOutput(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "zola.toml"), []byte(`
base_url = "https://example.com"
title = "Demo"
output_dir = "public"
compile_sass = false
generate_sitemap = true
generate_feeds = false
build_search_index = false
generate_robots_txt = true
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\n+++\nWelcome"), 0o644))
	for i := 0; i < 20; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(root, "content", "post-"+itoa(i)+".md"), []byte("+++\ntitle='Post'\n+++\nHello world"), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "page.html"), []byte("<html><body>{{ page.content|safe }}</body></html>"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "section.html"), []byte("<html><body>{{ section.title }}</body></html>"), 0o644))

	s, err := New(root, filepath.Join(root, "zola.toml"))
	require.NoError(t, err)
	require.NoError(t, s.Load(false))
	first := filepath.Join(root, "out-a")
	second := filepath.Join(root, "out-b")
	require.NoError(t, s.Build(BuildOptions{BuildMode: BuildDisk, OutputDir: first, Force: true, Concurrency: 4}))
	require.NoError(t, s.Build(BuildOptions{BuildMode: BuildDisk, OutputDir: second, Force: true, Concurrency: 4}))

	filesA := collectFiles(t, first)
	filesB := collectFiles(t, second)
	require.Equal(t, filesA, filesB)
	for _, rel := range filesA {
		a, err := os.ReadFile(filepath.Join(first, rel))
		require.NoError(t, err)
		b, err := os.ReadFile(filepath.Join(second, rel))
		require.NoError(t, err)
		require.Equal(t, string(a), string(b))
	}
}

func TestSiteBuild_SectionPagination(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "zola.toml"), []byte(`
base_url = "https://example.com"
title = "Demo"
output_dir = "public"
compile_sass = false
generate_sitemap = false
generate_feeds = false
build_search_index = false
generate_robots_txt = false
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\npaginate_by=2\npaginate_path='p'\n+++\nWelcome"), 0o644))
	for i := 1; i <= 5; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(root, "content", "post-"+itoa(i)+".md"), []byte("+++\ntitle='Post "+itoa(i)+"'\n+++\nHello world"), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "page.html"), []byte("<html><body>{{ page.content|safe }}</body></html>"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "section.html"), []byte("<html><body>{{ paginator.current_index }}/{{ paginator.number_pagers }}|{% for p in section.pages %}{{ p.title }};{% endfor %}</body></html>"), 0o644))

	s, err := New(root, filepath.Join(root, "zola.toml"))
	require.NoError(t, err)
	require.NoError(t, s.Load(false))
	require.NoError(t, s.Build(BuildOptions{BuildMode: BuildDisk, Force: true}))

	_, err = os.Stat(filepath.Join(root, "public", "index.html"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "public", "p", "2", "index.html"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "public", "p", "3", "index.html"))
	require.NoError(t, err)

	first, err := os.ReadFile(filepath.Join(root, "public", "index.html"))
	require.NoError(t, err)
	require.Contains(t, string(first), "1/3")
	require.Contains(t, string(first), "Post 1;")
	require.Contains(t, string(first), "Post 2;")

	third, err := os.ReadFile(filepath.Join(root, "public", "p", "3", "index.html"))
	require.NoError(t, err)
	require.Contains(t, string(third), "3/3")
	require.Contains(t, string(third), "Post 5;")
}

func TestSiteBuild_SectionPaginationReversedDefaultPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "zola.toml"), []byte(`
base_url = "https://example.com"
title = "Demo"
output_dir = "public"
compile_sass = false
generate_sitemap = false
generate_feeds = false
build_search_index = false
generate_robots_txt = false
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\npaginate_by=2\npaginate_reversed=true\n+++\nWelcome"), 0o644))
	for i := 1; i <= 3; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(root, "content", "post-"+itoa(i)+".md"), []byte("+++\ntitle='Post "+itoa(i)+"'\n+++\nHello world"), 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "page.html"), []byte("<html><body>{{ page.content|safe }}</body></html>"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "section.html"), []byte("<html><body>{% for p in section.pages %}{{ p.title }};{% endfor %}</body></html>"), 0o644))

	s, err := New(root, filepath.Join(root, "zola.toml"))
	require.NoError(t, err)
	require.NoError(t, s.Load(false))
	require.NoError(t, s.Build(BuildOptions{BuildMode: BuildDisk, Force: true}))

	_, err = os.Stat(filepath.Join(root, "public", "page", "2", "index.html"))
	require.NoError(t, err)

	first, err := os.ReadFile(filepath.Join(root, "public", "index.html"))
	require.NoError(t, err)
	require.Contains(t, string(first), "Post 3;")
	require.Contains(t, string(first), "Post 2;")
	require.NotContains(t, string(first), "Post 1;")
}

func collectFiles(t *testing.T, root string) []string {
	t.Helper()
	files := []string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	require.NoError(t, err)
	sort.Strings(files)
	return files
}
