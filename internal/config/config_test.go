package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromFile_AndThemeMerge(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "themes", "hyde"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "zola.toml"), []byte(`
base_url = "https://example.com"
theme = "hyde"
output_dir = "public"
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "themes", "hyde", "theme.toml"), []byte(`
title = "Theme Title"
description = "Theme Desc"
`), 0o644))

	cfg, err := FromFile(filepath.Join(root, "zola.toml"))
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", cfg.BaseURL)
	require.NoError(t, cfg.MergeTheme(filepath.Join(root, "themes", "hyde", "theme.toml")))
	assert.Equal(t, "Theme Title", cfg.Title)
	assert.Equal(t, "Theme Desc", cfg.Description)
}

func TestDiscoverConfigPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	nested := filepath.Join(root, "a", "b", "c")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "zola.toml"), []byte(`base_url="https://example.com"`), 0o644))

	r, cfg, err := DiscoverConfigPath(nested, "")
	require.NoError(t, err)
	assert.Equal(t, root, r)
	assert.Equal(t, filepath.Join(root, "zola.toml"), cfg)
}
