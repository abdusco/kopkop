package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOutputPathSafety(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		output string
		unsafe bool
	}{
		{name: "site", output: ".", unsafe: true},
		{name: "ancestor", output: "..", unsafe: true},
		{name: "filesystem root", output: string(filepath.Separator), unsafe: true},
		{name: "content", output: "content", unsafe: true},
		{name: "missing content child", output: "content/new/public", unsafe: true},
		{name: "templates", output: "templates", unsafe: true},
		{name: "static", output: "static", unsafe: true},
		{name: "theme", output: "themes/demo/public", unsafe: true},
		{name: "data", output: "data", unsafe: true},
		{name: "git", output: ".git", unsafe: true},
		{name: "config", output: "zola.toml", unsafe: true},
		{name: "watch path", output: "watched/public", unsafe: true},
		{name: "public", output: "public"},
		{name: "missing parents", output: "dist/site/public"},
		{name: "sibling prefix", output: "../site-public"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parent := t.TempDir()
			base := filepath.Join(parent, "site")
			require.NoError(t, os.MkdirAll(filepath.Join(base, "content"), 0o755))
			config := filepath.Join(base, "zola.toml")
			require.NoError(t, os.WriteFile(config, []byte("base_url='https://example.com'"), 0o644))
			sentinel := filepath.Join(base, "content", "post.md")
			require.NoError(t, os.WriteFile(sentinel, []byte("keep me"), 0o644))
			output := tc.output
			if !filepath.IsAbs(output) {
				output = filepath.Join(base, output)
			}
			err := validateOutputPath(base, config, output, []string{"watched"})
			if tc.unsafe {
				require.ErrorContains(t, err, "unsafe output directory")
			} else {
				require.NoError(t, err)
			}
			_, err = New(SiteParams{BasePath: base, ConfigPath: config, OutputDir: output})
			if tc.unsafe && tc.name != "watch path" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			data, err := os.ReadFile(sentinel)
			require.NoError(t, err)
			require.Equal(t, "keep me", string(data))
		})
	}
}

func TestBuildRechecksOutputSafety(t *testing.T) {
	t.Parallel()
	for _, mode := range []BuildMode{BuildDisk, BuildMemory, BuildBoth} {
		t.Run(map[BuildMode]string{BuildDisk: "disk", BuildMemory: "memory", BuildBoth: "both"}[mode], func(t *testing.T) {
			t.Parallel()
			base := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(base, "content"), 0o755))
			config := filepath.Join(base, "zola.toml")
			require.NoError(t, os.WriteFile(config, []byte("base_url='https://example.com'"), 0o644))
			sentinel := filepath.Join(base, "content", "post.md")
			require.NoError(t, os.WriteFile(sentinel, []byte("keep me"), 0o644))
			s, err := New(SiteParams{BasePath: base, ConfigPath: config})
			require.NoError(t, err)
			s.OutputPath = base
			require.ErrorContains(t, s.Build(BuildOptions{BuildMode: mode}), "unsafe output directory")
			require.FileExists(t, sentinel)
		})
	}
}

func TestOutputPathSymlinkSafety(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		target string
		output string
	}{
		{name: "site alias", target: ".", output: "link"},
		{name: "source alias", target: "content", output: "link"},
		{name: "missing child of source alias", target: "content", output: "link/missing/public"},
		{name: "ancestor alias", target: "..", output: "link"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base := filepath.Join(t.TempDir(), "site")
			require.NoError(t, os.MkdirAll(filepath.Join(base, "content"), 0o755))
			config := filepath.Join(base, "zola.toml")
			require.NoError(t, os.WriteFile(config, []byte("base_url='https://example.com'"), 0o644))
			sentinel := filepath.Join(base, "content", "post.md")
			require.NoError(t, os.WriteFile(sentinel, []byte("keep me"), 0o644))
			s, err := New(SiteParams{BasePath: base, ConfigPath: config, OutputDir: tc.output})
			require.NoError(t, err)
			require.NoError(t, os.Symlink(tc.target, filepath.Join(base, "link")))
			require.ErrorContains(t, s.Build(BuildOptions{}), "unsafe output directory")
			data, err := os.ReadFile(sentinel)
			require.NoError(t, err)
			require.Equal(t, "keep me", string(data))
		})
	}
}
