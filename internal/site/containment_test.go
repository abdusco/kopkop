package site

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildRejectsOutputTraversal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config string
		page   string
		bundle bool
	}{
		{name: "page path", page: "+++\npath='../outside/'\n+++\nBody"},
		{name: "alias", page: "+++\naliases=['../outside/sentinel']\n+++\nBody"},
		{name: "feed filename", config: "generate_feeds=true\nfeed_filenames=['../outside/sentinel']\n", page: "Body"},
		{name: "search path", config: "build_search_index=true\n[search]\nindex_path='../outside/sentinel'\n", page: "Body"},
		{name: "non-rendered bundle assets", page: "+++\npath='../outside/'\nrender=false\n+++\nBody", bundle: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, mode := range []BuildMode{BuildDisk, BuildMemory, BuildBoth} {
				base := t.TempDir()
				contentDir := filepath.Join(base, "content")
				pageName := "post.md"
				if tc.bundle {
					contentDir = filepath.Join(contentDir, "bundle")
					pageName = "index.md"
				}
				require.NoError(t, os.MkdirAll(contentDir, 0o755))
				require.NoError(t, os.MkdirAll(filepath.Join(base, "templates"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(base, "templates", "page.html"), []byte("{{ page.content | safe }}"), 0o644))
				require.NoError(t, os.MkdirAll(filepath.Join(base, "outside"), 0o755))
				sentinel := filepath.Join(base, "outside", "sentinel")
				require.NoError(t, os.WriteFile(sentinel, []byte("keep me"), 0o644))
				config := filepath.Join(base, "config.toml")
				require.NoError(t, os.WriteFile(config, []byte("base_url='https://example.com'\n"+tc.config), 0o644))
				require.NoError(t, os.WriteFile(filepath.Join(contentDir, pageName), []byte(tc.page), 0o644))
				if tc.bundle {
					require.NoError(t, os.WriteFile(filepath.Join(contentDir, "sentinel"), []byte("overwrite"), 0o644))
				}
				s, err := New(SiteParams{BasePath: base, ConfigPath: config})
				if tc.config != "" {
					require.ErrorIs(t, err, fs.ErrInvalid)
				} else {
					require.NoError(t, err)
					require.ErrorIs(t, s.Build(BuildOptions{BuildMode: mode}), fs.ErrInvalid)
				}
				data, err := os.ReadFile(sentinel)
				require.NoError(t, err)
				require.Equal(t, "keep me", string(data))
			}
		})
	}
}

func TestBuildRejectsEscapingSourceSymlinks(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"content/post.md", "content/bundle/sentinel", "static/sentinel", "templates/page.html", "themes/demo/theme.toml"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			parent := t.TempDir()
			base := filepath.Join(parent, "site")
			require.NoError(t, os.MkdirAll(filepath.Join(base, "content", "bundle"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(base, "content", "bundle", "index.md"), []byte("Body"), 0o644))
			if source != "templates/page.html" {
				require.NoError(t, os.MkdirAll(filepath.Join(base, "templates"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(base, "templates", "page.html"), []byte("{{ page.content | safe }}"), 0o644))
			}
			outside := filepath.Join(parent, "sentinel")
			require.NoError(t, os.WriteFile(outside, []byte("private source"), 0o644))
			config := filepath.Join(base, "config.toml")
			cfg := "base_url='https://example.com'\n"
			if source == "themes/demo/theme.toml" {
				cfg += "theme='demo'\n"
			}
			require.NoError(t, os.WriteFile(config, []byte(cfg), 0o644))
			link := filepath.Join(base, filepath.FromSlash(source))
			require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o755))
			require.NoError(t, os.Symlink(outside, link))
			s, err := New(SiteParams{BasePath: base, ConfigPath: config})
			if err == nil {
				err = s.Build(BuildOptions{})
			}
			require.Error(t, err)
			data, err := os.ReadFile(outside)
			require.NoError(t, err)
			require.Equal(t, "private source", string(data))
			if s != nil && s.MemoryOutput != nil {
				_, err := s.MemoryOutput.ReadFile("bundle/sentinel")
				require.Error(t, err)
			}
		})
	}
}
