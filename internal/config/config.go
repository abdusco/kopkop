package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/abdusco/kopkop/internal/filesystem"
)

type LinkCheckerLevel string

const (
	LinkCheckerWarn  LinkCheckerLevel = "warn"
	LinkCheckerError LinkCheckerLevel = "error"
)

type LinkChecker struct {
	InternalLevel      LinkCheckerLevel `toml:"internal_level"`
	SkipAnchorPrefixes []string         `toml:"skip_anchor_prefixes"`
	TimeoutSeconds     int              `toml:"timeout_seconds"`
	CacheFile          string           `toml:"cache_file"`
	UseCache           bool             `toml:"use_cache"`
}

type Markdown struct {
	InsertAnchorLinks        bool   `toml:"insert_anchor_links"`
	ExternalLinksTargetBlank bool   `toml:"external_links_target_blank"`
	HighlightTheme           string `toml:"highlight_theme"`
}

func (m *Markdown) UnmarshalTOML(v any) error {
	m.InsertAnchorLinks = false
	m.ExternalLinksTargetBlank = false
	m.HighlightTheme = ""
	obj, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	if raw, exists := obj["insert_anchor_links"]; exists {
		switch val := raw.(type) {
		case bool:
			m.InsertAnchorLinks = val
		case string:
			s := strings.ToLower(strings.TrimSpace(val))
			switch s {
			case "", "none", "false", "off", "0":
				m.InsertAnchorLinks = false
			default:
				m.InsertAnchorLinks = true
			}
		default:
			return fmt.Errorf("markdown.insert_anchor_links has unsupported type %T", raw)
		}
	}
	if raw, exists := obj["external_links_target_blank"]; exists {
		switch val := raw.(type) {
		case bool:
			m.ExternalLinksTargetBlank = val
		case string:
			s := strings.ToLower(strings.TrimSpace(val))
			m.ExternalLinksTargetBlank = s == "true" || s == "1" || s == "yes" || s == "on"
		default:
			return fmt.Errorf("markdown.external_links_target_blank has unsupported type %T", raw)
		}
	}
	if _, exists := obj["highlight_code"]; exists {
		return errors.New("markdown.highlight_code is no longer supported; use markdown.highlight_theme = \"...\"")
	}
	if _, exists := obj["highlighting"]; exists {
		return errors.New("markdown.highlighting is no longer supported; use markdown.highlight_theme = \"...\"")
	}
	if raw, exists := obj["highlight_theme"]; exists {
		theme, ok := raw.(string)
		if !ok {
			return fmt.Errorf("markdown.highlight_theme has unsupported type %T", raw)
		}
		m.HighlightTheme = strings.TrimSpace(theme)
	}

	return nil
}

type Search struct {
	BuildIndex bool   `toml:"build_index"`
	IndexPath  string `toml:"index_path"`
}

type TaxonomyConfig struct {
	Name string `toml:"name"`
	Feed bool   `toml:"feed"`
}

type Config struct {
	BaseURL             string           `toml:"base_url"`
	Title               string           `toml:"title"`
	Description         string           `toml:"description"`
	Author              string           `toml:"author"`
	Extra               map[string]any   `toml:"extra"`
	Theme               string           `toml:"theme"`
	OutputDir           string           `toml:"output_dir"`
	LinkStrategy        string           `toml:"link_strategy"`
	BuildSearchIndex    bool             `toml:"build_search_index"`
	GenerateFeeds       bool             `toml:"generate_feeds"`
	FeedFilenames       []string         `toml:"feed_filenames"`
	GenerateSitemap     bool             `toml:"generate_sitemap"`
	GenerateRobotsTXT   bool             `toml:"generate_robots_txt"`
	MinifyHTML          bool             `toml:"minify_html"`
	Taxonomies          []TaxonomyConfig `toml:"taxonomies"`
	Markdown            Markdown         `toml:"markdown"`
	Search              Search           `toml:"search"`
	LinkChecker         LinkChecker      `toml:"link_checker"`
	IgnoredContent      []string         `toml:"ignored_content"`
	ExtraWatchPaths     []string         `toml:"extra_watch_paths"`
	PathsKeepDates      bool             `toml:"paths_keep_dates"`
	EnableDraftsInBuild bool             `toml:"enable_drafts_in_build"`
}

func Default() Config {
	return Config{
		BaseURL:           "http://127.0.0.1:1111",
		OutputDir:         "public",
		LinkStrategy:      "absolute",
		BuildSearchIndex:  false,
		GenerateFeeds:     false,
		FeedFilenames:     []string{"atom.xml"},
		GenerateSitemap:   true,
		GenerateRobotsTXT: true,
		MinifyHTML:        false,
		Taxonomies:        []TaxonomyConfig{},
		Markdown: Markdown{
			InsertAnchorLinks:        false,
			ExternalLinksTargetBlank: false,
			HighlightTheme:           "",
		},
		Search: Search{
			BuildIndex: false,
			IndexPath:  "search_index.json",
		},
		LinkChecker: LinkChecker{
			InternalLevel:      LinkCheckerError,
			SkipAnchorPrefixes: []string{},
			TimeoutSeconds:     10,
			CacheFile:          ".kopkop-linkcheck-cache.json",
			UseCache:           true,
		},
		Extra:          map[string]any{},
		IgnoredContent: []string{},
	}
}

func FromFile(filename string) (Config, error) {
	cfg := Default()
	if _, err := toml.DecodeFile(filename, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", filename, err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) MergeTheme(themeTomlPath string) error {
	return c.MergeThemeFS(filesystem.NewDiskFS(filepath.Dir(themeTomlPath)), filepath.Base(themeTomlPath))
}

func (c *Config) MergeThemeFS(sourceFS fs.FS, themeTomlPath string) error {
	if c.Theme == "" {
		return nil
	}
	b, err := fs.ReadFile(sourceFS, themeTomlPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var themeCfg Config
	if _, err := toml.Decode(string(b), &themeCfg); err != nil {
		return fmt.Errorf("parse theme config %q: %w", themeTomlPath, err)
	}

	if c.Title == "" {
		c.Title = themeCfg.Title
	}
	if c.Description == "" {
		c.Description = themeCfg.Description
	}
	if len(c.Taxonomies) == 0 {
		c.Taxonomies = themeCfg.Taxonomies
	}
	if len(c.Extra) == 0 && len(themeCfg.Extra) > 0 {
		c.Extra = themeCfg.Extra
	}
	return nil
}

func (c Config) Validate() error {
	if c.Theme != "" {
		if err := filesystem.ValidatePath(c.Theme); err != nil {
			return fmt.Errorf("invalid theme path: %w", err)
		}
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		return errors.New("base_url must not be empty")
	}
	if _, err := url.Parse(c.BaseURL); err != nil {
		return fmt.Errorf("invalid base_url: %w", err)
	}
	if strings.TrimSpace(c.OutputDir) == "" {
		return errors.New("output_dir must not be empty")
	}
	if c.LinkStrategy == "" {
		c.LinkStrategy = "absolute"
	}
	switch strings.ToLower(strings.TrimSpace(c.LinkStrategy)) {
	case "absolute", "relative":
	default:
		return fmt.Errorf("link_strategy must be one of: absolute, relative")
	}
	if c.Search.IndexPath == "" {
		return errors.New("search.index_path must not be empty")
	}
	for _, f := range c.FeedFilenames {
		if strings.TrimSpace(f) == "" {
			return errors.New("feed_filenames must not contain empty values")
		}
	}
	return nil
}

func (c Config) TemplateView() map[string]any {
	linkStrategy := strings.TrimSpace(c.LinkStrategy)
	if linkStrategy == "" {
		linkStrategy = "absolute"
	}

	return map[string]any{
		"base_url":           c.BaseURL,
		"title":              c.Title,
		"description":        c.Description,
		"author":             c.Author,
		"output_dir":         c.OutputDir,
		"link_strategy":      linkStrategy,
		"build_search_index": c.BuildSearchIndex,
		"generate_feeds":     c.GenerateFeeds,
		"generate_sitemap":   c.GenerateSitemap,
		"extra":              c.Extra,
	}
}

func (c Config) MakePermalink(p string) string {
	base := strings.TrimRight(c.BaseURL, "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return base + p
}

func DiscoverConfigPath(startDir string, configArg string) (rootDir string, configPath string, err error) {
	if configArg != "" {
		for dir := startDir; dir != "." && dir != string(filepath.Separator); dir = filepath.Dir(dir) {
			cand := filepath.Join(dir, configArg)
			if _, statErr := os.Stat(cand); statErr == nil {
				abs, _ := filepath.Abs(cand)
				return dir, abs, nil
			}
			next := filepath.Dir(dir)
			if next == dir {
				break
			}
		}
		return "", "", fmt.Errorf("%s not found in current directory or ancestors", configArg)
	}

	for dir := startDir; ; dir = filepath.Dir(dir) {
		for _, name := range []string{"zola.toml", "config.toml"} {
			cand := filepath.Join(dir, name)
			if _, statErr := os.Stat(cand); statErr == nil {
				abs, _ := filepath.Abs(cand)
				return dir, abs, nil
			}
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
	}

	return "", "", errors.New("zola.toml (or config.toml) not found in current directory or ancestors")
}
