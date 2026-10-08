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
	require.NoError(t, os.WriteFile(filepath.Join(root, "config.toml"), []byte(`
base_url = "https://example.com"
theme = "hyde"
output_dir = "public"
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "themes", "hyde", "theme.toml"), []byte(`
title = "Theme Title"
description = "Theme Desc"
extra = { author = { name = "Keats" } }
`), 0o644))

	cfg, err := FromFile(filepath.Join(root, "config.toml"))
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
	require.NoError(t, os.WriteFile(filepath.Join(root, "config.toml"), []byte(`base_url="https://example.com"`), 0o644))

	r, cfg, err := DiscoverConfigPath(nested, "")
	require.NoError(t, err)
	assert.Equal(t, root, r)
	assert.Equal(t, filepath.Join(root, "config.toml"), cfg)
}

func TestFromFile_MarkdownOptions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(`
base_url = "https://example.com"

[markdown]
insert_anchor_links = "right"
external_links_target_blank = true
highlight_theme = "catppuccin-macchiato"
`), 0o644))

	cfg, err := FromFile(configPath)
	require.NoError(t, err)
	assert.True(t, cfg.Markdown.InsertAnchorLinks)
	assert.True(t, cfg.Markdown.ExternalLinksTargetBlank)
	assert.Equal(t, "catppuccin-macchiato", cfg.Markdown.HighlightTheme)
}

func TestFromFile_ExternalLinksTargetBlankValues(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		value   string
		want    bool
		wantErr string
	}{
		{name: "bool true", value: "true", want: true},
		{name: "bool false", value: "false", want: false},
		{name: "string yes", value: `"yes"`, want: true},
		{name: "string off", value: `"off"`, want: false},
		{name: "garbage string", value: `"garbage"`, wantErr: `"garbage" is not a boolean`},
		{name: "wrong type", value: "3", wantErr: "unsupported type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			configPath := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(configPath, []byte("base_url = \"https://example.com\"\n[markdown]\nexternal_links_target_blank = "+tc.value+"\n"), 0o644))

			cfg, err := FromFile(configPath)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, cfg.Markdown.ExternalLinksTargetBlank)
		})
	}
}

func TestFromFile_MarkdownHighlightSpellings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, markdown, wantTheme string
	}{
		{"highlight_code only", "[markdown]\nhighlight_code = true\n", "github"},
		{"highlight_code false", "[markdown]\nhighlight_code = false\nhighlight_theme = \"monokai\"\n", ""},
		{"highlight_code with theme", "[markdown]\nhighlight_code = true\nhighlight_theme = \"monokai\"\n", "monokai"},
		{"highlighting table", "[markdown.highlighting]\ntheme = \"dracula\"\n", "dracula"},
		{"highlighting table without theme", "[markdown.highlighting]\nstyle = \"inline\"\n", "github"},
		{"highlight_theme only", "[markdown]\nhighlight_theme = \"monokai\"\n", "monokai"},
		{"nothing", "[markdown]\ninsert_anchor_links = true\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			configPath := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(configPath, []byte("base_url = \"https://example.com\"\n"+tc.markdown), 0o644))

			cfg, err := FromFile(configPath)
			require.NoError(t, err)
			assert.Equal(t, tc.wantTheme, cfg.Markdown.HighlightTheme)
		})
	}
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
