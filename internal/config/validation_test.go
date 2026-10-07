package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name      string
		configure func(*Config)
		wantError string
	}{
		{"relative URL", func(c *Config) { c.BaseURL = "/blog" }, "absolute HTTP"},
		{"unsupported scheme", func(c *Config) { c.BaseURL = "ftp://example.com" }, "absolute HTTP"},
		{"missing host", func(c *Config) { c.BaseURL = "https:///blog" }, "absolute HTTP"},
		{"query", func(c *Config) { c.BaseURL = "https://example.com/?x=1" }, "query"},
		{"fragment", func(c *Config) { c.BaseURL = "https://example.com/#top" }, "fragment"},
		{"credentials", func(c *Config) { c.BaseURL = "https://user@example.com" }, "credentials"},
		{"severity", func(c *Config) { c.LinkChecker.InternalLevel = "ignore" }, "internal_level"},
		{"external severity", func(c *Config) { c.LinkChecker.ExternalLevel = "ignore" }, "external_level"},
		{"zero timeout", func(c *Config) { c.LinkChecker.TimeoutSeconds = 0 }, "timeout_seconds"},
		{"negative timeout", func(c *Config) { c.LinkChecker.TimeoutSeconds = -1 }, "timeout_seconds"},
		{"search escape", func(c *Config) { c.Search.IndexPath = "../index.json" }, "index_path"},
		{"feed escape", func(c *Config) { c.FeedFilenames = []string{"../atom.xml"} }, "feed_filenames"},
		{"valid subpath", func(c *Config) { c.BaseURL = "https://example.com/blog/" }, ""},
		{"normalization", func(c *Config) {
			c.BaseURL = " https://example.com/blog/ "
			c.LinkStrategy = " RELATIVE "
			c.LinkChecker.InternalLevel = " WARN "
		}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			tc.configure(&cfg)
			err := cfg.Validate()
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			if tc.name == "normalization" {
				require.Equal(t, "https://example.com/blog/", cfg.BaseURL)
				require.Equal(t, "relative", cfg.LinkStrategy)
				require.Equal(t, LinkCheckerWarn, cfg.LinkChecker.InternalLevel)
			}
		})
	}
}

func TestConfigUnsupportedKeys(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       []string
	}{
		{"unknown keys", "base_urll = 'typo'\n[search]\nbuild_indx = true\n[markdown]\nextra_grammars = ['x']", []string{"base_urll", "markdown.extra_grammars", "search.build_indx"}},
		{"supported and extra", "[markdown]\ninsert_anchor_links = 'right'\n[extra.custom]\nanything = 1", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(name, []byte(tc.body), 0o644))
			cfg, err := FromFile(name)
			require.NoError(t, err)
			require.Equal(t, tc.want, cfg.UnsupportedKeys)
		})
	}
}

func TestThemePreservesExplicitEmptySettings(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		empty      bool
	}{
		{"missing settings inherit", "", false},
		{"explicit empty settings", "title = ''\ndescription = ''\ntaxonomies = []\nextra = {}", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			name := filepath.Join(root, "config.toml")
			require.NoError(t, os.WriteFile(name, []byte("theme = 'sample'\n"+tc.body), 0o644))
			theme := filepath.Join(root, "theme.toml")
			require.NoError(t, os.WriteFile(theme, []byte("name = 'sample'\ntitle = 'Theme'\ndescription = 'Description'\ntaxonomies = [{name='tags'}]\nextra = {color='red'}"), 0o644))
			cfg, err := FromFile(name)
			require.NoError(t, err)
			require.NoError(t, cfg.MergeTheme(theme))
			if tc.empty {
				require.Empty(t, cfg.Title)
				require.Empty(t, cfg.Description)
				require.Empty(t, cfg.Taxonomies)
				require.Empty(t, cfg.Extra)
			} else {
				require.Equal(t, "Theme", cfg.Title)
				require.Equal(t, "Description", cfg.Description)
				require.Equal(t, []TaxonomyConfig{{Name: "tags"}}, cfg.Taxonomies)
				require.Equal(t, "red", cfg.Extra["color"])
			}
		})
	}
}

func TestExplicitConfigDiscovery(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	configFile := filepath.Join(root, "custom.toml")
	require.NoError(t, os.WriteFile(configFile, nil, 0o644))
	for _, tc := range []struct {
		name, start, arg, wantRoot, wantError string
	}{
		{"ancestor", nested, "custom.toml", root, ""},
		{"absolute", nested, configFile, root, ""},
		{"root directory searched", string(filepath.Separator), strings.TrimPrefix(configFile, string(filepath.Separator)), string(filepath.Separator), ""},
		{"relative start", ".", configFile, root, ""},
		{"missing explicit has no fallback", root, "missing.toml", "", "not found"},
		{"directory rejected", root, "a", "", "regular file"},
		{"absolute directory rejected", nested, root, "", "regular file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotRoot, gotConfig, err := DiscoverConfigPath(tc.start, tc.arg)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantRoot, gotRoot)
			require.Equal(t, configFile, gotConfig)
		})
	}
}
