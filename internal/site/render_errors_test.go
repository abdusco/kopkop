package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildTemplateFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{name: "missing page", files: map[string]string{"content/post.md": "Body"}, want: []string{"post.md", "page.html", "post/index.html", "not found"}},
		{name: "missing selected page", files: map[string]string{"content/post.md": "+++\ntemplate='missing.html'\n+++\nBody", "templates/page.html": "OK"}, want: []string{"post.md", "missing.html", "not found"}},
		{name: "page runtime error", files: map[string]string{"content/post.md": "Body", "templates/page.html": "{{ broken() }}"}, want: []string{"post.md", "page.html", "unknown function"}},
		{name: "data failure", files: map[string]string{"content/post.md": "Body", "templates/page.html": "{{ load_data('missing.json') }}"}, want: []string{"post.md", "page.html", "missing.json"}},
		{name: "missing section", files: map[string]string{"content/_index.md": "Body"}, want: []string{"_index.md", "section.html", "not found"}},
		{name: "section runtime error", files: map[string]string{"content/blog/_index.md": "Body", "templates/section.html": "{{ broken() }}"}, want: []string{"blog/_index.md", "section.html", "blog/index.html", "unknown function"}},
		{name: "homepage runtime error", files: map[string]string{"templates/index.html": "{{ broken() }}"}, want: []string{"homepage", "index.html", "unknown function"}},
		{name: "unpublished page", files: map[string]string{"content/post.md": "+++\nrender=false\ntemplate='missing.html'\n+++\nBody"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, mode := range []BuildMode{BuildDisk, BuildMemory, BuildBoth} {
				root := t.TempDir()
				require.NoError(t, os.MkdirAll(filepath.Join(root, "content"), 0o755))
				cfg := filepath.Join(root, "zola.toml")
				require.NoError(t, os.WriteFile(cfg, []byte("base_url='https://example.com'\n"), 0o644))
				for name, body := range tc.files {
					p := filepath.Join(root, filepath.FromSlash(name))
					require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
					require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
				}
				s, err := New(SiteParams{BasePath: root, ConfigPath: cfg})
				require.NoError(t, err)
				err = s.Build(BuildOptions{BuildMode: mode, Force: true})
				if len(tc.want) == 0 {
					require.NoError(t, err)
					require.NotEmpty(t, s.Library.Pages["post.md"].Content)
					continue
				}
				require.Error(t, err)
				for _, want := range tc.want {
					require.Contains(t, err.Error(), want)
				}
			}
		})
	}
}

func TestPageTemplateErrorsAreDeterministic(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
	cfg := filepath.Join(root, "zola.toml")
	require.NoError(t, os.WriteFile(cfg, []byte("base_url='https://example.com'\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "page.html"), []byte("{{ broken() }}"), 0o644))
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "content", name), []byte("Body"), 0o644))
	}
	s, err := New(SiteParams{BasePath: root, ConfigPath: cfg})
	require.NoError(t, err)
	for range 10 {
		err := s.Build(BuildOptions{BuildMode: BuildMemory})
		require.ErrorContains(t, err, `render page "a.md"`)
		files, err := s.MemoryOutput.ReadDir(".")
		require.NoError(t, err)
		require.Empty(t, files)
	}
}
