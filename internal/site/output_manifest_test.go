package site

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildRejectsOutputCollisionsBeforeRendering(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		files  map[string]string
		path   string
		owners []string
	}{
		{
			name: "duplicate page paths", path: "same/index.html", owners: []string{"page a.md", "page b.md"},
			files: map[string]string{"content/a.md": "+++\npath='same'\n+++\n{{ missing() }}", "content/b.md": "+++\npath='same'\n+++\nBody"},
		},
		{
			name: "page and section", path: "blog/index.html", owners: []string{"page blog.md", "section blog/_index.md"},
			files: map[string]string{"content/blog.md": "Body", "content/blog/_index.md": "Section"},
		},
		{
			name: "page and homepage", path: "index.html", owners: []string{"page post.md", "implicit homepage"},
			files: map[string]string{"content/post.md": "+++\npath='/'\n+++\nBody", "templates/index.html": "Home"},
		},
		{
			name: "page alias", path: "post/index.html", owners: []string{"page post.md", "alias /post/ of page other.md"},
			files: map[string]string{"content/post.md": "Body", "content/other.md": "+++\naliases=['/post/']\n+++\nBody"},
		},
		{
			name: "section alias", path: "post/index.html", owners: []string{"page post.md", "alias /post/ of section _index.md"},
			files: map[string]string{"content/post.md": "Body", "content/_index.md": "+++\naliases=['/post/']\n+++\nSection"},
		},
		{
			name: "html aliases", path: "old.html", owners: []string{"alias /old.html of page a.md", "alias old.html of page b.md"},
			files: map[string]string{"content/a.md": "+++\naliases=['/old.html']\n+++\nBody", "content/b.md": "+++\naliases=['old.html']\n+++\nBody"},
		},
		{
			name: "pagination alias", path: "page/1/index.html", owners: []string{"pagination alias of section _index.md", "page post.md"},
			files: map[string]string{"content/_index.md": "+++\npaginate_by=1\n+++\nSection", "content/post.md": "+++\npath='page/1'\n+++\nBody"},
		},
		{
			name: "pagination page", path: "page/2/index.html", owners: []string{"section _index.md", "page post.md"},
			files: map[string]string{"content/_index.md": "+++\npaginate_by=1\n+++\nSection", "content/post.md": "+++\npath='page/2'\n+++\nBody", "content/other.md": "Body"},
		},
		{
			name: "taxonomy term slugs", path: "tags/go-lang/index.html", owners: []string{"term Go Lang", "term Go-Lang"},
			config: "[[taxonomies]]\nname='tags'\n",
			files:  map[string]string{"content/post.md": "+++\n[taxonomies]\ntags=['Go Lang','Go-Lang']\n+++\nBody"},
		},
		{
			name: "taxonomy names", path: "go-lang/index.html", owners: []string{"taxonomy Go Lang", "taxonomy Go-Lang"},
			config: "[[taxonomies]]\nname='Go Lang'\n[[taxonomies]]\nname='Go-Lang'\n",
			files:  map[string]string{"content/post.md": "+++\n[taxonomies]\n'Go Lang'=['A']\n'Go-Lang'=['B']\n+++\nBody"},
		},
		{
			name: "taxonomy and page", path: "tags/index.html", owners: []string{"taxonomy tags", "page tags.md"},
			config: "[[taxonomies]]\nname='tags'\n",
			files:  map[string]string{"content/tags.md": "+++\n[taxonomies]\ntags=['Go']\n+++\nBody"},
		},
		{
			name: "static and page", path: "post/index.html", owners: []string{"page post.md", "static static/post/index.html"},
			files: map[string]string{"content/post.md": "Body", "static/post/index.html": "Static"},
		},
		{
			name: "bundled asset and page", path: "bundle/index.html", owners: []string{"page bundle/index.md", "asset content/bundle/index.html"},
			files: map[string]string{"content/bundle/index.md": "Body", "content/bundle/index.html": "Asset"},
		},
		{
			name: "bundled asset and static", path: "bundle/photo.png", owners: []string{"asset content/bundle/photo.png", "static static/bundle/photo.png"},
			files: map[string]string{"content/bundle/index.md": "Body", "content/bundle/photo.png": "Asset", "static/bundle/photo.png": "Static"},
		},
		{
			name: "static file and generated directory", path: "post", owners: []string{"page post.md", "static static/post"},
			files: map[string]string{"content/post.md": "Body", "static/post": "Static"},
		},
		{
			name: "static directory and generated file", path: "404.html", owners: []string{"generated 404 page", "static static/404.html"},
			files: map[string]string{"static/404.html/nested": "Static"},
		},
		{
			name: "case insensitive aliases", path: "old.html", owners: []string{"alias OLD.html", "alias old.html"},
			files: map[string]string{"content/a.md": "+++\naliases=['OLD.html']\n+++\nBody", "content/b.md": "+++\naliases=['old.html']\n+++\nBody"},
		},
		{
			name: "feed and sitemap", path: "sitemap.xml", owners: []string{"site feed sitemap.xml", "generated sitemap"},
			config: "generate_feeds=true\nfeed_filenames=['sitemap.xml']\n",
		},
		{
			name: "section feed and static", path: "blog/atom.xml", owners: []string{"feed atom.xml of section blog/_index.md", "static static/blog/atom.xml"},
			config: "generate_feeds=true\n",
			files:  map[string]string{"content/blog/_index.md": "+++\ngenerate_feed=true\n+++\nSection", "static/blog/atom.xml": "Static"},
		},
		{
			name: "taxonomy feed and static", path: "tags/go/atom.xml", owners: []string{"feed atom.xml of term Go", "static static/tags/go/atom.xml"},
			config: "[[taxonomies]]\nname='tags'\nfeed=true\n",
			files:  map[string]string{"content/post.md": "+++\n[taxonomies]\ntags=['Go']\n+++\nBody", "static/tags/go/atom.xml": "Static"},
		},
		{
			name: "search index and robots", path: "robots.txt", owners: []string{"search index", "generated robots.txt"},
			config: "build_search_index=true\n[search]\nindex_path='robots.txt'\n",
		},
		{
			name: "reserved highlight stylesheet", path: "code-github.css", owners: []string{"highlight stylesheet", "static static/code-github.css"},
			config: "[markdown]\nhighlight_theme='github'\n",
			files:  map[string]string{"static/code-github.css": "Static"},
		},
		{
			name: "theme directory and site file", path: "assets", owners: []string{"static themes/demo/static/assets", "static static/assets"},
			config: "theme='demo'\n",
			files:  map[string]string{"themes/demo/theme.toml": "name='Demo'", "themes/demo/static/assets/style.css": "Theme", "static/assets": "Site"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []BuildMode{BuildDisk, BuildMemory, BuildBoth} {
				t.Run(fmt.Sprint(mode), func(t *testing.T) {
					root := t.TempDir()
					require.NoError(t, os.MkdirAll(filepath.Join(root, "content"), 0o755))
					files := map[string]string{
						"config.toml":              "base_url='https://example.com'\ngenerate_sitemap=true\ngenerate_robots_txt=true\n" + tc.config,
						"templates/page.html":    `{% include "missing-page.html" %}`,
						"templates/section.html": `{% include "missing-section.html" %}`,
						"public/sentinel":        "previous output",
					}
					for name, body := range tc.files {
						files[name] = body
					}
					for name, body := range files {
						require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
						require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
					}
					s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "config.toml")})
					require.NoError(t, err)
					var first string
					for i := 0; i < 3; i++ {
						err = s.Build(BuildOptions{BuildMode: mode})
						require.ErrorContains(t, err, "output collision")
						require.ErrorContains(t, err, tc.path)
						for _, owner := range tc.owners {
							require.ErrorContains(t, err, owner)
						}
						if i == 0 {
							first = err.Error()
						} else {
							require.EqualError(t, err, first)
						}
					}
					body, err := os.ReadFile(filepath.Join(root, "public/sentinel"))
					require.NoError(t, err)
					require.Equal(t, "previous output", string(body))
					entries, err := os.ReadDir(filepath.Join(root, "public"))
					require.NoError(t, err)
					require.Len(t, entries, 1)
				})
			}
		})
	}
}

func TestOutputManifestAllowsDistinctOutputsAndSiteStaticOverrides(t *testing.T) {
	for _, mode := range []BuildMode{BuildDisk, BuildMemory, BuildBoth} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			root := t.TempDir()
			for name, body := range map[string]string{
				"config.toml":                           "base_url='https://example.com'\ntheme='demo'\ngenerate_feeds=true\n",
				"themes/demo/theme.toml":              "name='Demo'",
				"themes/demo/static/assets/style.css": "Theme stylesheet",
				"themes/demo/static/assets/theme.css": "Theme only",
				"static/assets/style.css":             "Site stylesheet",
				"content/_index.md":                   "+++\npaginate_by=1\n+++\nHome",
				"content/post.md":                     "+++\naliases=['old']\n+++\nBody",
				"content/hidden.md":                   "+++\npath='post'\nrender=false\naliases=['old']\n+++\nHidden",
				"content/other.md":                    "Body",
				"templates/page.html":                 "{{ page.content|safe }}",
				"templates/section.html":              "{{ section.content|safe }}",
			} {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
			}
			s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "config.toml")})
			require.NoError(t, err)
			require.NoError(t, s.Build(BuildOptions{BuildMode: mode}))
			body, err := s.OutputFS.ReadFile("assets/style.css")
			require.NoError(t, err)
			require.Equal(t, "Site stylesheet", string(body))
			body, err = s.OutputFS.ReadFile("assets/theme.css")
			require.NoError(t, err)
			require.Equal(t, "Theme only", string(body))
		})
	}
}
