package site

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/abdusco/kopkop/internal/assets"
	"github.com/abdusco/kopkop/internal/config"
	"github.com/abdusco/kopkop/internal/content"
	"github.com/abdusco/kopkop/internal/linkcheck"
	"github.com/abdusco/kopkop/internal/markdown"
	"github.com/abdusco/kopkop/internal/markdown/shortcode"
	"github.com/abdusco/kopkop/internal/search"
	"github.com/abdusco/kopkop/internal/templates"
)

type BuildMode int

const (
	BuildDisk BuildMode = iota
	BuildMemory
	BuildBoth
)

type BuildOptions struct {
	IncludeDrafts bool
	BaseURL       string
	OutputDir     string
	BuildMode     BuildMode
	LiveReloadURL string
	Minify        bool
	Force         bool
	Concurrency   int
}

type Site struct {
	BasePath      string
	ConfigPath    string
	Config        config.Config
	Templates     *templates.Manager
	Library       *content.Library
	OutputPath    string
	BuildMode     BuildMode
	MemoryContent map[string]string
}

var htmlSpaceRe = regexp.MustCompile(`\s+`)

func New(basePath string, configPath string) (*Site, error) {
	cfg, err := config.FromFile(configPath)
	if err != nil {
		return nil, err
	}
	if cfg.Theme != "" {
		themeToml := filepath.Join(basePath, "themes", cfg.Theme, "theme.toml")
		if err := cfg.MergeTheme(themeToml); err != nil {
			return nil, err
		}
	}

	tplMgr, err := templates.LoadManager(basePath, cfg.Theme)
	if err != nil {
		return nil, err
	}

	outputPath := filepath.Join(basePath, cfg.OutputDir)
	return &Site{
		BasePath:      basePath,
		ConfigPath:    configPath,
		Config:        cfg,
		Templates:     tplMgr,
		OutputPath:    outputPath,
		BuildMode:     BuildDisk,
		MemoryContent: map[string]string{},
	}, nil
}

func (s *Site) Load(includeDrafts bool) error {
	lib, err := content.LoadLibrary(s.BasePath, s.Config, content.LoadOptions{IncludeDrafts: includeDrafts, RenderMarkdown: false})
	if err != nil {
		return err
	}
	s.Library = lib
	return nil
}

func (s *Site) Build(opts BuildOptions) error {
	if opts.BaseURL != "" {
		s.Config.BaseURL = opts.BaseURL
	}
	if opts.OutputDir != "" {
		s.OutputPath = opts.OutputDir
	}
	s.BuildMode = opts.BuildMode
	if opts.Minify {
		s.Config.MinifyHTML = true
	}

	if s.Library == nil {
		if err := s.Load(opts.IncludeDrafts); err != nil {
			return err
		}
	}

	if s.BuildMode == BuildDisk || s.BuildMode == BuildBoth {
		if !opts.Force {
			if _, err := os.Stat(s.OutputPath); err == nil {
				return fmt.Errorf("directory %q already exists; use --force to overwrite", s.OutputPath)
			}
		}
		if err := assets.CleanOutput(s.OutputPath); err != nil {
			return err
		}
	}

	if s.Config.CompileSass {
		if s.Config.Theme != "" {
			themeSass := filepath.Join(s.BasePath, "themes", s.Config.Theme, "sass")
			if err := assets.CompileSassDir(themeSass, s.OutputPath); err != nil {
				return err
			}
		}
		if err := assets.CompileSass(s.BasePath, s.OutputPath); err != nil {
			return err
		}
	}

	if err := s.renderAllPages(opts.LiveReloadURL, opts.Concurrency); err != nil {
		return err
	}
	if err := s.renderAliases(); err != nil {
		return err
	}
	if err := s.renderSections(opts.LiveReloadURL); err != nil {
		return err
	}
	if err := s.renderTaxonomies(opts.LiveReloadURL); err != nil {
		return err
	}
	if s.Config.GenerateSitemap {
		if err := s.renderSitemap(); err != nil {
			return err
		}
	}
	if s.Config.GenerateFeeds {
		if err := s.renderFeed(); err != nil {
			return err
		}
	}
	if err := s.render404(opts.LiveReloadURL); err != nil {
		return err
	}
	if s.Config.GenerateRobotsTXT {
		if err := s.renderRobots(); err != nil {
			return err
		}
	}

	if s.Config.BuildSearchIndex || s.Config.Search.BuildIndex {
		if err := search.BuildIndex(s.Library, s.OutputPath, s.Config.Search.IndexPath); err != nil {
			return err
		}
	}

	if err := s.copyStatic(); err != nil {
		return err
	}
	if err := s.copyColocatedAssets(); err != nil {
		return err
	}

	return nil
}

func (s *Site) CheckExternalLinks() []linkcheck.Result {
	return linkcheck.CheckExternalLinks(s.Library, s.Config.LinkChecker)
}

type pageRenderArtifact struct {
	Path string
	HTML string
	Page *content.Page
	Err  error
}

func (s *Site) renderAllPages(liveReloadURL string, concurrency int) error {
	defs := s.Templates.ShortcodeDefinitions()
	paths := make([]string, 0, len(s.Library.Pages))
	for p := range s.Library.Pages {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if concurrency <= 0 {
		concurrency = 1
	}

	jobs := make(chan string)
	results := make(chan pageRenderArtifact, len(paths))
	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		for rel := range jobs {
			pg := s.Library.Pages[rel]
			rendered, err := s.renderMarkdownWithShortcodes(pg, defs)
			if err != nil {
				results <- pageRenderArtifact{Err: fmt.Errorf("render markdown %s: %w", rel, err)}
				continue
			}
			pg.Content = rendered.Body
			pg.Summary = rendered.Summary
			pg.ExternalLinks = rendered.ExternalLinks
			pg.InternalLinks = make([]content.InternalLink, 0, len(rendered.InternalLinks))
			for _, il := range rendered.InternalLinks {
				pg.InternalLinks = append(pg.InternalLinks, content.InternalLink{Path: il.Path, Anchor: il.Anchor})
			}
			pg.TOC = make([]content.Heading, 0, len(rendered.TOC))
			for _, h := range rendered.TOC {
				pg.TOC = append(pg.TOC, content.Heading{ID: h.ID, Level: h.Level, Title: h.Title})
			}

			tplName := "page.html"
			if pg.Meta.Template != "" {
				tplName = pg.Meta.Template
			} else if sec, ok := s.Library.Sections[pg.ParentSection]; ok && sec.Meta.PageTemplate != "" {
				tplName = sec.Meta.PageTemplate
			}
			ctx := s.baseTemplateContext(pg.Lang)
			ctx["page"] = s.pageView(rel, pg)
			ctx["lang"] = pg.Lang
			ctx["current_url"] = pg.Permalink
			ctx["current_path"] = pg.Path
			html, err := s.Templates.Render(tplName, ctx)
			if err != nil {
				html = "<html><body>" + pg.Content + "</body></html>"
			}
			html = injectLiveReload(html, liveReloadURL)
			results <- pageRenderArtifact{Path: filepath.Join(strings.TrimPrefix(pg.Path, "/"), "index.html"), HTML: html, Page: pg}
		}
	}

	if concurrency > len(paths) {
		concurrency = len(paths)
	}
	if concurrency < 1 {
		concurrency = 1
	}
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go worker()
	}
	for _, rel := range paths {
		jobs <- rel
	}
	close(jobs)
	wg.Wait()
	close(results)

	artifacts := make(map[string]pageRenderArtifact, len(paths))
	for r := range results {
		if r.Err != nil {
			return r.Err
		}
		artifacts[r.Path] = r
	}

	for _, rel := range paths {
		pg := s.Library.Pages[rel]
		outPath := filepath.Join(strings.TrimPrefix(pg.Path, "/"), "index.html")
		artifact := artifacts[outPath]
		if err := s.writeOutput(outPath, artifact.HTML); err != nil {
			return err
		}
	}
	return nil
}

func (s *Site) baseTemplateContext(lang string) map[string]any {
	ctx := map[string]any{
		"config": map[string]any{
			"base_url":         s.Config.BaseURL,
			"title":            s.Config.Title,
			"description":      s.Config.Description,
			"default_language": s.Config.DefaultLanguage,
			"extra":            s.Config.Extra,
		},
		"__pages":        s.serializedPages(),
		"__sections":     s.serializedSections(),
		"__taxonomies":   s.serializedTaxonomies(),
		"__translations": map[string]any{},
		"lang":           lang,
	}
	return ctx
}

func (s *Site) pageView(rel string, pg *content.Page) map[string]any {
	trans := make([]map[string]any, 0, len(pg.Translations))
	for _, tRel := range pg.Translations {
		if tp, ok := s.Library.Pages[tRel]; ok {
			trans = append(trans, map[string]any{
				"path":      tRel,
				"lang":      tp.Lang,
				"title":     tp.Meta.Title,
				"permalink": tp.Permalink,
			})
		}
	}
	return map[string]any{
		"title":         pg.Meta.Title,
		"description":   pg.Meta.Description,
		"content":       pg.Content,
		"path":          pg.Path,
		"relative_path": pg.RelativePath,
		"permalink":     pg.Permalink,
		"lang":          pg.Lang,
		"toc":           pg.TOC,
		"summary":       pg.Summary,
		"slug":          pg.Slug,
		"date":          pg.Date,
		"earlier":       map[string]any{"permalink": "", "title": ""},
		"later":         map[string]any{"permalink": "", "title": ""},
		"translations":  trans,
		"assets":        pg.Assets,
		"taxonomies":    pg.Meta.Taxonomies,
		"aliases":       pg.Meta.Aliases,
		"draft":         pg.Meta.Draft,
	}
}

func (s *Site) renderMarkdownWithShortcodes(pg *content.Page, defs map[string]templates.ShortcodeDefinition) (markdown.Rendered, error) {
	out, scs, err := shortcode.Parse(pg.RawContent)
	if err != nil {
		return markdown.Rendered{}, err
	}

	contentWithMD, htmlSCs, err := shortcode.InsertMarkdownShortcodes(out, scs,
		func(sc shortcode.Shortcode) (string, error) {
			def, ok := defs[sc.Name]
			if !ok {
				return "", fmt.Errorf("unknown shortcode: %s", sc.Name)
			}
			if def.FileType != "md" {
				return shortcode.Placeholder, nil
			}
			ctx := map[string]any{"nth": sc.Nth}
			for k, v := range sc.Args {
				ctx[k] = v
			}
			if sc.Body != nil {
				ctx["body"] = strings.TrimRight(*sc.Body, "\n")
			}
			return s.Templates.Engine.Render(def.Template, ctx)
		},
		func(sc shortcode.Shortcode) bool {
			def, ok := defs[sc.Name]
			return ok && def.FileType == "md"
		},
	)
	if err != nil {
		return markdown.Rendered{}, err
	}

	rendered, err := markdown.RenderContent(contentWithMD, markdown.RenderContext{
		Permalinks:           s.Library.Permalinks,
		CurrentPagePath:      pg.RelativePath,
		CurrentPagePermalink: pg.Permalink,
	})
	if err != nil {
		return markdown.Rendered{}, err
	}

	for _, sc := range htmlSCs {
		def, ok := defs[sc.Name]
		if !ok {
			return markdown.Rendered{}, fmt.Errorf("unknown shortcode: %s", sc.Name)
		}
		ctx := map[string]any{"nth": sc.Nth}
		for k, v := range sc.Args {
			ctx[k] = v
		}
		if sc.Body != nil {
			ctx["body"] = strings.TrimRight(*sc.Body, "\n")
		}
		repl, rErr := s.Templates.Engine.Render(def.Template, ctx)
		if rErr != nil {
			return markdown.Rendered{}, rErr
		}
		rendered.Body = strings.Replace(rendered.Body, shortcode.Placeholder, repl, 1)
	}

	return rendered, nil
}

func (s *Site) renderSections(liveReloadURL string) error {
	paths := make([]string, 0, len(s.Library.Sections))
	for p := range s.Library.Sections {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		sec := s.Library.Sections[rel]
		tpl := "section.html"
		if sec.Meta.Template != "" {
			tpl = sec.Meta.Template
		}
		pages := make([]map[string]any, 0, len(sec.Pages))
		for _, p := range sec.Pages {
			pg := s.Library.Pages[p]
			pages = append(pages, map[string]any{"title": pg.Meta.Title, "permalink": pg.Permalink, "path": pg.Path})
		}
		ctx := s.baseTemplateContext(sec.Lang)
		ctx["section"] = map[string]any{
			"title":         sec.Meta.Title,
			"description":   sec.Meta.Description,
			"path":          sec.Path,
			"relative_path": sec.RelativePath,
			"permalink":     sec.Permalink,
			"pages":         pages,
			"subsections":   sec.Subsections,
			"content":       sec.Content,
		}
		html, err := s.Templates.Render(tpl, ctx)
		if err != nil {
			html = "<html><body><h1>" + sec.Meta.Title + "</h1></body></html>"
		}
		html = injectLiveReload(html, liveReloadURL)
		if err := s.writeOutput(filepath.Join(strings.TrimPrefix(sec.Path, "/"), "index.html"), html); err != nil {
			return err
		}
	}
	return nil
}

func (s *Site) renderTaxonomies(liveReloadURL string) error {
	for _, tax := range s.Library.Taxonomies {
		for termName, term := range tax.Terms {
			entries := make([]map[string]any, 0, len(term.Pages))
			for _, rel := range term.Pages {
				pg := s.Library.Pages[rel]
				entries = append(entries, map[string]any{"title": pg.Meta.Title, "permalink": pg.Permalink})
			}
			pathSlug := strings.ReplaceAll(strings.ToLower(termName), " ", "-")
			ctx := s.baseTemplateContext(s.Config.DefaultLanguage)
			ctx["taxonomy"] = map[string]any{"name": tax.Name, "term": termName, "pages": entries}
			html, err := s.Templates.Render("taxonomy_single.html", ctx)
			if err != nil {
				html = "<html><body><h1>" + tax.Name + ": " + termName + "</h1></body></html>"
			}
			html = injectLiveReload(html, liveReloadURL)
			if err := s.writeOutput(filepath.Join(tax.Name, pathSlug, "index.html"), html); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Site) renderSitemap() error {
	urls := make([]string, 0, len(s.Library.Permalinks))
	for _, v := range s.Library.Permalinks {
		urls = append(urls, v)
	}
	sort.Strings(urls)
	content, err := s.Templates.Render("sitemap.xml", map[string]any{"pages": urls})
	if err != nil {
		content = "<?xml version=\"1.0\"?><urlset></urlset>"
	}
	return s.writeOutput("sitemap.xml", content)
}

func (s *Site) renderFeed() error {
	pages := make([]map[string]any, 0, len(s.Library.Pages))
	for _, p := range s.Library.Pages {
		pages = append(pages, map[string]any{"title": p.Meta.Title, "permalink": p.Permalink})
	}
	sort.SliceStable(pages, func(i, j int) bool {
		return pages[i]["permalink"].(string) < pages[j]["permalink"].(string)
	})
	content, err := s.Templates.Render("rss.xml", map[string]any{"pages": pages, "config": map[string]any{"title": s.Config.Title}})
	if err != nil {
		content = "<?xml version=\"1.0\"?><rss version=\"2.0\"></rss>"
	}
	return s.writeOutput("rss.xml", content)
}

func (s *Site) render404(liveReloadURL string) error {
	content, err := s.Templates.Render("404.html", map[string]any{"config": map[string]any{"base_url": s.Config.BaseURL}})
	if err != nil {
		content = "<html><body><h1>404</h1></body></html>"
	}
	content = injectLiveReload(content, liveReloadURL)
	return s.writeOutput("404.html", content)
}

func (s *Site) renderRobots() error {
	content, err := s.Templates.Render("robots.txt", map[string]any{"config": map[string]any{"base_url": s.Config.BaseURL}})
	if err != nil {
		content = "User-agent: *\nAllow: /\n"
	}
	return s.writeOutput("robots.txt", content)
}

func (s *Site) renderAliases() error {
	for _, p := range s.Library.Pages {
		for _, alias := range p.Meta.Aliases {
			redirect, err := s.Templates.Engine.Render("__zola_builtins/internal/alias.html", map[string]any{"url": p.Permalink})
			if err != nil {
				redirect = "<meta http-equiv=\"refresh\" content=\"0; url=" + p.Permalink + "\">"
			}
			aliasPath := strings.TrimPrefix(alias, "/")
			file := "index.html"
			if strings.HasSuffix(aliasPath, ".html") {
				file = filepath.Base(aliasPath)
				aliasPath = filepath.Dir(aliasPath)
			}
			if aliasPath == "." {
				aliasPath = ""
			}
			if err := s.writeOutput(filepath.Join(aliasPath, file), redirect); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Site) copyStatic() error {
	staticDir := filepath.Join(s.BasePath, "static")
	if s.Config.Theme != "" {
		themeStatic := filepath.Join(s.BasePath, "themes", s.Config.Theme, "static")
		if err := assets.CopyDirectory(themeStatic, s.OutputPath); err != nil {
			return err
		}
	}
	return assets.CopyDirectory(staticDir, s.OutputPath)
}

func (s *Site) copyColocatedAssets() error {
	for _, p := range s.Library.Pages {
		for _, asset := range p.Assets {
			relName := filepath.Base(asset)
			destDir := filepath.Join(s.OutputPath, strings.TrimPrefix(p.Path, "/"))
			if err := os.MkdirAll(destDir, 0o755); err != nil {
				return err
			}
			b, err := os.ReadFile(asset)
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(destDir, relName), b, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Site) writeOutput(rel string, content string) error {
	rel = filepath.Clean(rel)
	if strings.HasPrefix(rel, "../") {
		return fmt.Errorf("invalid output path %q", rel)
	}
	if strings.HasPrefix(rel, "/") {
		rel = strings.TrimPrefix(rel, "/")
	}

	if s.Config.MinifyHTML && strings.HasSuffix(strings.ToLower(rel), ".html") {
		content = minifyHTML(content)
	}

	if s.BuildMode == BuildMemory || s.BuildMode == BuildBoth {
		s.MemoryContent[filepath.ToSlash(rel)] = content
	}
	if s.BuildMode == BuildDisk || s.BuildMode == BuildBoth {
		full := filepath.Join(s.OutputPath, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func minifyHTML(in string) string {
	trimmed := strings.TrimSpace(in)
	trimmed = htmlSpaceRe.ReplaceAllString(trimmed, " ")
	trimmed = strings.ReplaceAll(trimmed, "> <", "><")
	return trimmed
}

func injectLiveReload(html string, reloadURL string) string {
	if reloadURL == "" {
		return html
	}
	script := `<script>(function(){var ws=new WebSocket("` + reloadURL + `");ws.onmessage=function(){window.location.reload();};})();</script>`
	if strings.Contains(html, "</body>") {
		return strings.Replace(html, "</body>", script+"</body>", 1)
	}
	return html + script
}

func (s *Site) serializedPages() map[string]any {
	out := map[string]any{}
	for rel, pg := range s.Library.Pages {
		out[rel] = s.pageView(rel, pg)
	}
	return out
}

func (s *Site) serializedSections() map[string]any {
	out := map[string]any{}
	for rel, sec := range s.Library.Sections {
		pages := make([]map[string]any, 0, len(sec.Pages))
		for _, p := range sec.Pages {
			if pg, ok := s.Library.Pages[p]; ok {
				pages = append(pages, map[string]any{"title": pg.Meta.Title, "permalink": pg.Permalink, "path": pg.Path})
			}
		}
		out[rel] = map[string]any{
			"title":         sec.Meta.Title,
			"description":   sec.Meta.Description,
			"path":          sec.Path,
			"relative_path": sec.RelativePath,
			"permalink":     sec.Permalink,
			"pages":         pages,
			"subsections":   sec.Subsections,
			"content":       sec.Content,
		}
	}
	return out
}

func (s *Site) serializedTaxonomies() map[string]any {
	out := map[string]any{}
	for name, tx := range s.Library.Taxonomies {
		terms := map[string]any{}
		for termName, term := range tx.Terms {
			pages := make([]map[string]any, 0, len(term.Pages))
			for _, rel := range term.Pages {
				if pg, ok := s.Library.Pages[rel]; ok {
					pages = append(pages, map[string]any{"title": pg.Meta.Title, "permalink": pg.Permalink, "path": pg.Path})
				}
			}
			terms[termName] = map[string]any{"name": termName, "pages": pages}
		}
		out[name] = map[string]any{"name": name, "terms": terms}
	}
	return out
}
