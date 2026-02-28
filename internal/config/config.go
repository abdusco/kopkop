package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
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
}

type Markdown struct {
	InsertAnchorLinks bool `toml:"insert_anchor_links"`
}

type Search struct {
	BuildIndex bool   `toml:"build_index"`
	IndexPath  string `toml:"index_path"`
}

type TaxonomyConfig struct {
	Name string `toml:"name"`
}

type LanguageOptions struct {
	Title            string `toml:"title"`
	BuildSearchIndex bool   `toml:"build_search_index"`
	GenerateFeeds    bool   `toml:"generate_feeds"`
}

type Config struct {
	BaseURL             string                     `toml:"base_url"`
	Title               string                     `toml:"title"`
	Description         string                     `toml:"description"`
	Theme               string                     `toml:"theme"`
	OutputDir           string                     `toml:"output_dir"`
	DefaultLanguage     string                     `toml:"default_language"`
	Languages           map[string]LanguageOptions `toml:"languages"`
	CompileSass         bool                       `toml:"compile_sass"`
	BuildSearchIndex    bool                       `toml:"build_search_index"`
	GenerateFeeds       bool                       `toml:"generate_feeds"`
	GenerateSitemap     bool                       `toml:"generate_sitemap"`
	GenerateRobotsTXT   bool                       `toml:"generate_robots_txt"`
	MinifyHTML          bool                       `toml:"minify_html"`
	Taxonomies          []TaxonomyConfig           `toml:"taxonomies"`
	Markdown            Markdown                   `toml:"markdown"`
	Search              Search                     `toml:"search"`
	LinkChecker         LinkChecker                `toml:"link_checker"`
	IgnoredContent      []string                   `toml:"ignored_content"`
	ExtraWatchPaths     []string                   `toml:"extra_watch_paths"`
	PathsKeepDates      bool                       `toml:"paths_keep_dates"`
	EnableDraftsInBuild bool                       `toml:"enable_drafts_in_build"`
}

func Default() Config {
	return Config{
		BaseURL:           "http://127.0.0.1:1111",
		OutputDir:         "public",
		DefaultLanguage:   "en",
		CompileSass:       true,
		BuildSearchIndex:  false,
		GenerateFeeds:     false,
		GenerateSitemap:   true,
		GenerateRobotsTXT: true,
		MinifyHTML:        false,
		Taxonomies:        []TaxonomyConfig{},
		Markdown: Markdown{
			InsertAnchorLinks: true,
		},
		Search: Search{
			BuildIndex: false,
			IndexPath:  "search_index.json",
		},
		LinkChecker: LinkChecker{
			InternalLevel:      LinkCheckerError,
			SkipAnchorPrefixes: []string{},
			TimeoutSeconds:     10,
		},
		Languages:      map[string]LanguageOptions{},
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
	if c.Theme == "" {
		return nil
	}
	if _, err := os.Stat(themeTomlPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var themeCfg Config
	if _, err := toml.DecodeFile(themeTomlPath, &themeCfg); err != nil {
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
	return nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.BaseURL) == "" {
		return errors.New("base_url must not be empty")
	}
	if _, err := url.Parse(c.BaseURL); err != nil {
		return fmt.Errorf("invalid base_url: %w", err)
	}
	if strings.TrimSpace(c.OutputDir) == "" {
		return errors.New("output_dir must not be empty")
	}
	if strings.TrimSpace(c.DefaultLanguage) == "" {
		return errors.New("default_language must not be empty")
	}
	if c.Search.IndexPath == "" {
		return errors.New("search.index_path must not be empty")
	}
	return nil
}

func (c Config) MakePermalink(p string) string {
	base := strings.TrimRight(c.BaseURL, "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return base + p
}

func (c Config) OtherLanguages() map[string]LanguageOptions {
	out := map[string]LanguageOptions{}
	for k, v := range c.Languages {
		if k == c.DefaultLanguage {
			continue
		}
		out[k] = v
	}
	return out
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
