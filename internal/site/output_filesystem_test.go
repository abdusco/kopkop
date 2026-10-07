package site

import (
	"bytes"
	"image"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/abdusco/kopkop/internal/filesystem"
	"github.com/stretchr/testify/require"
)

func TestBuildModesProduceCompleteMatchingArtifacts(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"zola.toml":                    "base_url='https://example.com'\ntheme='demo'\nbuild_search_index=true\n[markdown]\nhighlight_theme='github'\n",
		"themes/demo/theme.toml":       "name='Demo'",
		"themes/demo/static/theme.css": "Theme",
		"themes/demo/static/style.css": "Theme style",
		"static/style.css":             "Site style",
		"static/nested/file.txt":       "Static",
		"content/bundle/index.md":      "+++\ntitle='Bundle'\naliases=['old']\n+++\n```go\nvar x = 1\n```",
		"content/bundle/asset.txt":     "Bundle asset",
		"templates/page.html":          `<html><head></head><body>{{ page.content|safe }}<img src="{{ resize_image(path="data/logo.png", width=1, height=1, op="scale").url }}"></body></html>`,
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
	}
	var encoded bytes.Buffer
	require.NoError(t, png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "data"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "data/logo.png"), encoded.Bytes(), 0o644))
	s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "zola.toml")})
	require.NoError(t, err)
	var expected map[string][]byte
	for _, tc := range []struct {
		name string
		mode BuildMode
	}{
		{"disk", BuildDisk}, {"memory", BuildMemory}, {"both", BuildBoth}, {"memory again", BuildMemory}, {"disk again", BuildDisk},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, s.MemoryOutput.WriteFile("stale.txt", []byte("stale"), 0o644))
			if tc.mode == BuildMemory {
				require.NoError(t, os.WriteFile(filepath.Join(s.OutputPath, "disk-sentinel"), []byte("keep"), 0o644))
			}
			require.NoError(t, s.Build(BuildOptions{BuildMode: tc.mode}))
			artifacts := map[string][]byte{}
			require.NoError(t, fs.WalkDir(s.OutputFS, ".", func(name string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() {
					return nil
				}
				body, err := s.OutputFS.ReadFile(name)
				artifacts[name] = body
				return err
			}))
			for _, name := range []string{"bundle/index.html", "bundle/asset.txt", "old/index.html", "theme.css", "style.css", "nested/file.txt", "search_index.json", "code-github.css", "404.html"} {
				require.Contains(t, artifacts, name)
			}
			processed := []string{}
			for name := range artifacts {
				if filepath.Dir(name) == "processed_images" {
					processed = append(processed, name)
				}
			}
			require.Len(t, processed, 1)
			require.Regexp(t, `^processed_images/[0-9a-f]{64}-1x1\.png$`, processed[0])
			require.Equal(t, []byte("Site style"), artifacts["style.css"])
			if expected == nil {
				expected = artifacts
			} else {
				require.Equal(t, expected, artifacts)
			}
			if tc.mode == BuildBoth {
				memoryArtifacts := map[string][]byte{}
				require.NoError(t, fs.WalkDir(s.MemoryOutput, ".", func(name string, entry fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if entry.IsDir() {
						return nil
					}
					body, err := s.MemoryOutput.ReadFile(name)
					memoryArtifacts[name] = body
					return err
				}))
				require.Equal(t, artifacts, memoryArtifacts)
			}
			if tc.mode == BuildDisk {
				entries, err := fs.ReadDir(s.MemoryOutput, ".")
				require.NoError(t, err)
				require.Empty(t, entries)
			}
			if tc.mode == BuildMemory {
				body, err := os.ReadFile(filepath.Join(s.OutputPath, "disk-sentinel"))
				require.NoError(t, err)
				require.Equal(t, "keep", string(body))
			}
		})
	}
}

func TestMemoryBuildDoesNotCreateOutputDirectory(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "static"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "static/file"), []byte("Static"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "zola.toml"), []byte("base_url='https://example.com'\nbuild_search_index=true"), 0o644))
	s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "zola.toml")})
	require.NoError(t, err)
	for _, mode := range []BuildMode{-1, 3} {
		require.ErrorContains(t, s.Build(BuildOptions{BuildMode: mode}), "invalid build mode")
		require.NoDirExists(t, s.OutputPath)
	}
	require.NoError(t, s.Build(BuildOptions{BuildMode: BuildMemory}))
	require.NoDirExists(t, s.OutputPath)
	require.IsType(t, &filesystem.MemoryFS{}, s.OutputFS)
}
