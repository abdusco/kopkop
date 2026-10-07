package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/abdusco/kopkop/internal/filesystem"
	"github.com/abdusco/kopkop/internal/templates"
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
		{name: "missing include", files: map[string]string{"content/post.md": "Body", "templates/page.html": "{% include 'missing.html' %}"}, want: []string{"post.md", "page.html", "missing.html"}},
		{name: "shortcode failure", files: map[string]string{"content/post.md": "{{ broken() }}", "templates/shortcodes/broken.html": "{{ unknown() }}"}, want: []string{"post.md", "shortcodes/broken.html", "unknown function"}},
		{name: "missing section", files: map[string]string{"content/_index.md": "Body"}, want: []string{"_index.md", "section.html", "not found"}},
		{name: "missing selected section", files: map[string]string{"content/_index.md": "+++\ntemplate='missing.html'\n+++\nBody", "templates/section.html": "OK"}, want: []string{"_index.md", "missing.html", "not found"}},
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
				err = s.Build(BuildOptions{BuildMode: mode})
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

func TestAuxiliaryTemplateFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		files         map[string]string
		breakRedirect bool
		want          []string
	}{
		{name: "builtin defaults"},
		{name: "404 override", files: map[string]string{"templates/404.html": "{{ broken() }}"}, want: []string{"404.html", "unknown function"}},
		{name: "robots override", files: map[string]string{"templates/robots.txt": "{{ broken() }}"}, want: []string{"robots.txt", "unknown function"}},
		{name: "taxonomy list override", files: map[string]string{"templates/tags/list.html": "{{ broken() }}", "templates/taxonomy_list.html": "OK"}, want: []string{"taxonomy", "tags", "tags/list.html", "unknown function"}},
		{name: "taxonomy term override", files: map[string]string{"templates/tags/single.html": "{{ broken() }}", "templates/taxonomy_single.html": "OK"}, want: []string{"taxonomy", "tags", "Example", "tags/single.html", "unknown function"}},
		{name: "page redirect", files: map[string]string{"content/post.md": "+++\nredirect_to='/target/'\n+++\nBody"}, breakRedirect: true, want: []string{"page redirect", "post.md", "post/index.html", "internal/alias.html"}},
		{name: "section redirect", files: map[string]string{"content/_index.md": "+++\nredirect_to='/target/'\n+++\nBody"}, breakRedirect: true, want: []string{"section redirect", "_index.md", "index.html", "internal/alias.html"}},
		{name: "page alias", files: map[string]string{"content/post.md": "+++\naliases=['old-post']\n+++\nBody"}, breakRedirect: true, want: []string{"alias", "old-post", "post.md", "internal/alias.html"}},
		{name: "section alias", files: map[string]string{"content/_index.md": "+++\naliases=['old-home']\n+++\nBody"}, breakRedirect: true, want: []string{"alias", "old-home", "_index.md", "internal/alias.html"}},
		{name: "pagination alias", files: map[string]string{"content/_index.md": "+++\npaginate_by=1\n+++\nBody"}, breakRedirect: true, want: []string{"pagination alias", "_index.md", "internal/alias.html"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, mode := range []BuildMode{BuildDisk, BuildMemory, BuildBoth} {
				root := t.TempDir()
				cfg := filepath.Join(root, "zola.toml")
				require.NoError(t, os.WriteFile(cfg, []byte("base_url='https://example.com'\ngenerate_robots_txt=true\ntaxonomies=[{name='tags'}]\n"), 0o644))
				files := map[string]string{
					"content/post.md":        "+++\ntitle='Post'\ntaxonomies={tags=['Example']}\n+++\nBody",
					"content/_index.md":      "Body",
					"templates/page.html":    "{{ page.content | safe }}",
					"templates/section.html": "{{ section.content | safe }}",
				}
				for name, body := range tc.files {
					files[name] = body
				}
				for name, body := range files {
					p := filepath.Join(root, filepath.FromSlash(name))
					require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
					require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
				}
				s, err := New(SiteParams{BasePath: root, ConfigPath: cfg})
				require.NoError(t, err)
				if tc.breakRedirect {
					require.NoError(t, s.Templates.Engine.AddTemplate("__zola_builtins/internal/alias.html", "{{ broken() }}"))
				}
				err = s.Build(BuildOptions{BuildMode: mode})
				if len(tc.want) == 0 {
					require.NoError(t, err)
					var output filesystem.FileSystem = filesystem.NewDiskFS(s.OutputPath)
					if mode == BuildMemory {
						output = s.MemoryOutput
					}
					for name, want := range map[string]string{"404.html": "404 Not Found", "robots.txt": "User-agent:", "tags/index.html": "Example", "tags/example/index.html": "Category: Example"} {
						body, err := output.ReadFile(name)
						require.NoError(t, err)
						require.Contains(t, string(body), want)
					}
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

func TestRenderFirstTemplateSelection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		files     map[string]string
		want      string
		wantError string
	}{
		{name: "first present", files: map[string]string{"templates/first.html": "first", "templates/second.html": "second"}, want: "first"},
		{name: "missing first", files: map[string]string{"templates/second.html": "second"}, want: "second"},
		{name: "broken first", files: map[string]string{"templates/first.html": "{{ broken() }}", "templates/second.html": "second"}, wantError: "first.html"},
		{name: "no candidates", wantError: "no matching template"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := filesystem.NewMemoryFS()
			for name, body := range tc.files {
				require.NoError(t, source.WriteFile(name, []byte(body), 0o644))
			}
			manager, err := templates.LoadManagerFS(source, filesystem.NewMemoryFS(), "")
			require.NoError(t, err)
			s := &Site{Templates: manager}
			out, err := s.renderFirstTemplate([]string{"first.html", "second.html"}, nil)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				require.Empty(t, out)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, out)
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
