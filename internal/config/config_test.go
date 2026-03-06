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
extra = { author = { name = "Keats" } }
`), 0o644))

	cfg, err := FromFile(filepath.Join(root, "zola.toml"))
	require.NoError(t, err)
	assert.Equal(t, "https://example.com", cfg.BaseURL)
	require.NoError(t, cfg.MergeTheme(filepath.Join(root, "themes", "hyde", "theme.toml")))
	assert.Equal(t, "Theme Title", cfg.Title)
	assert.Equal(t, "Theme Desc", cfg.Description)
	assert.Equal(t, "Keats", cfg.Extra["author"].(map[string]any)["name"])
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

func TestFromFile_MarkdownBooleanAndStringOptions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configPath := filepath.Join(root, "zola.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(`
base_url = "https://example.com"

[markdown]
insert_anchor_links = "right"
external_links_target_blank = true
highlight_code = true
`), 0o644))

	cfg, err := FromFile(configPath)
	require.NoError(t, err)
	assert.True(t, cfg.Markdown.InsertAnchorLinks)
	assert.True(t, cfg.Markdown.ExternalLinksTargetBlank)
	assert.True(t, cfg.Markdown.HighlightCode)
}

func TestConfigValidate_LinkStrategy(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.BaseURL = "https://example.com"
	cfg.OutputDir = "public"
	cfg.Search.IndexPath = "search_index.json"

	cfg.LinkStrategy = "relative"
	require.NoError(t, cfg.Validate())

	cfg.LinkStrategy = "absolute"
	require.NoError(t, cfg.Validate())

	cfg.LinkStrategy = "weird"
	require.Error(t, cfg.Validate())
}

