package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/abdusco/kopkop/internal/filesystem"
	"github.com/abdusco/kopkop/internal/slug"
)

type LinkCheckerLevel string

const (
	LinkCheckerWarn  LinkCheckerLevel = "warn"
	LinkCheckerError LinkCheckerLevel = "error"
)

type LinkChecker struct {
	InternalLevel      LinkCheckerLevel `toml:"internal_level"`
	ExternalLevel      LinkCheckerLevel `toml:"external_level"`
	SkipAnchorPrefixes []string         `toml:"skip_anchor_prefixes"`
	TimeoutSeconds     int              `toml:"timeout_seconds"`
	CacheFile          string           `toml:"cache_file"`
	UseCache           bool             `toml:"use_cache"`
	CacheTTLSeconds    int              `toml:"cache_ttl_seconds"`
	Refresh            bool             `toml:"-"`
}

// defaultHighlightTheme is used when a Zola config enables highlighting
// without naming a Chroma style.
const defaultHighlightTheme = "github"

type Markdown struct {
	InsertAnchorLinks        bool   `toml:"insert_anchor_links"`
	ExternalLinksTargetBlank bool   `toml:"external_links_target_blank"`
	HighlightTheme           string `toml:"highlight_theme"`
	unsupportedKeys          []string
}

func (m *Markdown) UnmarshalTOML(v any) error {
	m.InsertAnchorLinks = false
	m.ExternalLinksTargetBlank = false
	m.HighlightTheme = ""
	m.unsupportedKeys = nil
	obj, ok := v.(map[string]any)
	if !ok {
		return errors.New("markdown must be a table")
	}
	for key := range obj {
		switch key {
		case "insert_anchor_links", "external_links_target_blank", "highlight_theme", "highlight_code", "highlighting":
		default:
			m.unsupportedKeys = append(m.unsupportedKeys, "markdown."+key)
		}
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
	// Zola spellings: [markdown.highlighting] theme = "..." and highlight_code = bool.
	if raw, exists := obj["highlighting"]; exists {
		table, ok := raw.(map[string]any)
		if !ok {
			return errors.New("markdown.highlighting must be a table")
		}
		for _, key := range []string{"theme", "dark_theme", "light_theme"} {
			if theme, ok := table[key].(string); ok && strings.TrimSpace(theme) != "" {
				m.HighlightTheme = strings.TrimSpace(theme)
				break
			}
		}
		if m.HighlightTheme == "" {
			m.HighlightTheme = defaultHighlightTheme
		}
	}
	highlightCode := true
	if raw, exists := obj["highlight_code"]; exists {
		val, ok := raw.(bool)
		if !ok {
			return fmt.Errorf("markdown.highlight_code has unsupported type %T", raw)
		}
		highlightCode = val
		if val && m.HighlightTheme == "" {
			m.HighlightTheme = defaultHighlightTheme
		}
	}
	if raw, exists := obj["highlight_theme"]; exists {
		theme, ok := raw.(string)
		if !ok {
			return fmt.Errorf("markdown.highlight_theme has unsupported type %T", raw)
		}
		m.HighlightTheme = strings.TrimSpace(theme)
	}
	if !highlightCode {
		m.HighlightTheme = ""
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
	// UnsupportedKeys reports ignored settings for compatibility with Zola configs.
	UnsupportedKeys     []string `toml:"-"`
	metadata            *toml.MetaData
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
	FeedLimit           int              `toml:"feed_limit"`
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
			ExternalLevel:      LinkCheckerError,
			SkipAnchorPrefixes: []string{},
			TimeoutSeconds:     10,
			CacheFile:          ".kopkop-linkcheck-cache.json",
			UseCache:           true,
			CacheTTLSeconds:    86400,
		},
		Extra:          map[string]any{},
		IgnoredContent: []string{},
	}
}

func FromFile(filename string) (Config, error) {
	cfg := Default()
	metadata, err := toml.DecodeFile(filename, &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", filename, err)
	}
	cfg.metadata = &metadata
	for _, key := range metadata.Undecoded() {
		if len(key) > 0 && key[0] == "extra" {
			continue
		}
		cfg.UnsupportedKeys = append(cfg.UnsupportedKeys, key.String())
	}
	cfg.UnsupportedKeys = append(cfg.UnsupportedKeys, cfg.Markdown.unsupportedKeys...)
	sort.Strings(cfg.UnsupportedKeys)
	cfg.UnsupportedKeys = slices.Compact(cfg.UnsupportedKeys)
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("config %q: %w", filename, err)
	}
	if len(cfg.UnsupportedKeys) > 0 {
		log.Printf("config %q: unsupported settings ignored: %s", filename, strings.Join(cfg.UnsupportedKeys, ", "))
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

	if c.Title == "" && (c.metadata == nil || !c.metadata.IsDefined("title")) {
		c.Title = themeCfg.Title
	}
	if c.Description == "" && (c.metadata == nil || !c.metadata.IsDefined("description")) {
		c.Description = themeCfg.Description
	}
	if len(c.Taxonomies) == 0 && (c.metadata == nil || !c.metadata.IsDefined("taxonomies")) {
		c.Taxonomies = themeCfg.Taxonomies
	}
	if len(c.Extra) == 0 && len(themeCfg.Extra) > 0 && (c.metadata == nil || !c.metadata.IsDefined("extra")) {
		c.Extra = themeCfg.Extra
	}
	return c.Validate()
}

func (c *Config) Validate() error {
	c.BaseURL = strings.TrimSpace(c.BaseURL)
	c.LinkStrategy = strings.ToLower(strings.TrimSpace(c.LinkStrategy))
	c.LinkChecker.InternalLevel = LinkCheckerLevel(strings.ToLower(strings.TrimSpace(string(c.LinkChecker.InternalLevel))))
	c.LinkChecker.ExternalLevel = LinkCheckerLevel(strings.ToLower(strings.TrimSpace(string(c.LinkChecker.ExternalLevel))))
	if c.Theme != "" {
		if err := filesystem.ValidatePath(c.Theme); err != nil {
			return fmt.Errorf("invalid theme path: %w", err)
		}
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		return errors.New("base_url must not be empty")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return fmt.Errorf("invalid base_url: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.Opaque != "" {
		return errors.New("base_url must be an absolute HTTP or HTTPS URL with a host")
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return errors.New("base_url must not contain credentials, a query, or a fragment")
	}
	c.BaseURL = u.String()
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
	if err := filesystem.ValidatePath(c.Search.IndexPath); err != nil || c.Search.IndexPath == "." {
		return fmt.Errorf("search.index_path must be a relative output file path: %w", fs.ErrInvalid)
	}
	switch c.LinkChecker.InternalLevel {
	case LinkCheckerWarn, LinkCheckerError:
	default:
		return errors.New("link_checker.internal_level must be one of: warn, error")
	}
	switch c.LinkChecker.ExternalLevel {
	case LinkCheckerWarn, LinkCheckerError:
	default:
		return errors.New("link_checker.external_level must be one of: warn, error")
	}
	if c.LinkChecker.TimeoutSeconds <= 0 {
		return errors.New("link_checker.timeout_seconds must be positive")
	}
	if c.LinkChecker.CacheTTLSeconds < 0 {
		return errors.New("link_checker.cache_ttl_seconds must not be negative")
	}
	if c.LinkChecker.UseCache && strings.TrimSpace(c.LinkChecker.CacheFile) == "" {
		return errors.New("link_checker.cache_file must not be empty when caching is enabled")
	}
	if c.FeedLimit < 0 {
		return errors.New("feed_limit must not be negative")
	}
	for _, f := range c.FeedFilenames {
		if strings.TrimSpace(f) == "" {
			return errors.New("feed_filenames must not contain empty values")
		}
		if err := filesystem.ValidatePath(f); err != nil || f == "." {
			return fmt.Errorf("feed_filenames must contain relative output file paths: %w", fs.ErrInvalid)
		}
	}
	for _, taxonomy := range c.Taxonomies {
		if slug.Normalize(taxonomy.Name) == "" {
			return errors.New("taxonomy names must contain a letter or number")
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
	startDir, err = filepath.Abs(startDir)
	if err != nil {
		return "", "", err
	}
	if filepath.IsAbs(configArg) {
		configArg = filepath.Clean(configArg)
		info, err := os.Stat(configArg)
		if err != nil {
			return "", "", fmt.Errorf("find config %q: %w", configArg, err)
		}
		if !info.Mode().IsRegular() {
			return "", "", fmt.Errorf("config %q must be a regular file", configArg)
		}
		return filepath.Dir(configArg), configArg, nil
	}

	names := []string{"zola.toml", "config.toml"}
	if configArg != "" {
		names = []string{configArg}
	}
	for dir := startDir; ; dir = filepath.Dir(dir) {
		for _, name := range names {
			cand := filepath.Join(dir, name)
			info, statErr := os.Stat(cand)
			if statErr == nil {
				if !info.Mode().IsRegular() {
					return "", "", fmt.Errorf("config %q must be a regular file", cand)
				}
				return dir, cand, nil
			}
			if !errors.Is(statErr, fs.ErrNotExist) {
				return "", "", fmt.Errorf("find config %q: %w", cand, statErr)
			}
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
	}

	if configArg != "" {
		return "", "", fmt.Errorf("%s not found in current directory or ancestors", configArg)
	}
	return "", "", errors.New("zola.toml (or config.toml) not found in current directory or ancestors")
}
