package site

import (
	"bytes"
	"cmp"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/abdusco/kopkop/internal/assets"
	"github.com/abdusco/kopkop/internal/config"
	"github.com/abdusco/kopkop/internal/content"
	"github.com/abdusco/kopkop/internal/filesystem"
	"github.com/abdusco/kopkop/internal/linkcheck"
	"github.com/abdusco/kopkop/internal/markdown"
	"github.com/abdusco/kopkop/internal/markdown/shortcode"
	"github.com/abdusco/kopkop/internal/search"
	"github.com/abdusco/kopkop/internal/slug"
	"github.com/abdusco/kopkop/internal/templates"
	"github.com/samber/lo"
	"github.com/sourcegraph/conc/pool"
	"github.com/tdewolff/minify/v2"
	minifyhtml "github.com/tdewolff/minify/v2/html"
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
	BuildMode     BuildMode
	LiveReloadURL string
	Minify        bool
	Force         bool
	Concurrency   int
}

type Site struct {
	params       SiteParams
	BasePath     string
	ConfigPath   string
	Config       config.Config
	Templates    *templates.Manager
	Library      *content.Library
	OutputPath   string
	OutputFS     filesystem.FileSystem
	BuildMode    BuildMode
	MemoryOutput *filesystem.MemoryFS

	templateViews       *templateViews
	highlightCSSPath    string
	highlightCSSWritten bool
}

type SiteParams struct {
	BasePath   string
	ConfigPath string
	OutputDir  string
}

var htmlMinifier = func() *minify.M {
	m := minify.New()
	m.AddFunc("text/html", minifyhtml.Minify)
	return m
}()

var shortcodeParagraphRe = regexp.MustCompile(`(?s)<p>\s*` + regexp.QuoteMeta(shortcode.Placeholder) + `\s*</p>`)
var continueReadingMarkerRe = regexp.MustCompile(`(?s)<span\s+id=["']continue-reading["']\s*></span>`)

func New(params SiteParams) (*Site, error) {
	cfg, err := config.FromFile(params.ConfigPath)
	if err != nil {
		return nil, err
	}
	sourceFS := filesystem.NewDiskFS(params.BasePath)
	if cfg.Theme != "" {
		themeToml := filepath.ToSlash(filepath.Join("themes", cfg.Theme, "theme.toml"))
		if err := cfg.MergeThemeFS(sourceFS, themeToml); err != nil {
			return nil, err
		}
	}

	if params.OutputDir != "" {
		cfg.OutputDir = params.OutputDir
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	outputPath := cfg.OutputDir
	if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(params.BasePath, outputPath)
	}
	if err := validateOutputPath(params.BasePath, params.ConfigPath, outputPath, cfg.ExtraWatchPaths); err != nil {
		return nil, err
	}

	outputFS := filesystem.NewDiskFS(outputPath)

	tplMgr, err := templates.LoadManagerFS(sourceFS, outputFS, cfg.Theme)
	if err != nil {
		return nil, err
	}

	return &Site{
		params:       params,
		BasePath:     params.BasePath,
		ConfigPath:   params.ConfigPath,
		Config:       cfg,
		Templates:    tplMgr,
		OutputPath:   outputPath,
		OutputFS:     outputFS,
		BuildMode:    BuildDisk,
		MemoryOutput: filesystem.NewMemoryFS(),
	}, nil
}

// Reload constructs an independent site with fresh configuration and templates.
// It preserves construction overrides and leaves the receiver untouched on failure.
func (s *Site) Reload() (*Site, error) {
	return New(s.params)
}

func (s *Site) Load(includeDrafts bool) error {
	lib, err := content.LoadLibrary(s.BasePath, s.Config, content.LoadOptions{IncludeDrafts: includeDrafts, RenderMarkdown: false})
	if err != nil {
		return err
	}
	s.Library = lib
	s.templateViews = nil
	return nil
}

func (s *Site) Build(opts BuildOptions) error {
	if opts.Concurrency < 0 {
		return fmt.Errorf("concurrency must not be negative")
	}
	if opts.BuildMode < BuildDisk || opts.BuildMode > BuildBoth {
		return fmt.Errorf("invalid build mode: %d", opts.BuildMode)
	}
	s.templateViews = nil
	if err := validateOutputPath(s.BasePath, s.ConfigPath, s.OutputPath, s.Config.ExtraWatchPaths); err != nil {
		return err
	}
	s.highlightCSSPath = ""
	s.highlightCSSWritten = false
	if strings.TrimSpace(s.Config.Markdown.HighlightTheme) != "" {
		s.highlightCSSPath = highlightStylesheetFilename(s.Config.Markdown.HighlightTheme)
	}

	if opts.BaseURL != "" {
		cfg := s.Config
		cfg.BaseURL = opts.BaseURL
		if err := cfg.Validate(); err != nil {
			return err
		}
		if cfg.BaseURL != s.Config.BaseURL {
			// Derived permalinks must be rebuilt using the effective configuration.
			s.Library = nil
		}
		s.Config = cfg
	}
	s.BuildMode = opts.BuildMode
	s.MemoryOutput = filesystem.NewMemoryFS()
	s.OutputFS = filesystem.NewDiskFS(s.OutputPath)
	if s.BuildMode == BuildMemory {
		s.OutputFS = s.MemoryOutput
	} else if s.BuildMode == BuildBoth {
		s.OutputFS = &filesystem.MirrorFS{Primary: s.OutputFS, Mirror: s.MemoryOutput}
	}
	s.Templates.OutputFS = s.OutputFS
	s.Templates.ConfigureHelpers()
	if opts.Minify {
		s.Config.MinifyHTML = true
	}

	if s.Library == nil {
		if err := s.Load(opts.IncludeDrafts); err != nil {
			return err
		}
	}
	if err := s.validateOutputManifest(); err != nil {
		return err
	}

	if s.BuildMode == BuildDisk || s.BuildMode == BuildBoth {
		// Recheck immediately before deletion, including symlinks changed since New.
		if err := validateOutputPath(s.BasePath, s.ConfigPath, s.OutputPath, s.Config.ExtraWatchPaths); err != nil {
			return err
		}
		if err := assets.CleanOutput(s.OutputPath); err != nil {
			return err
		}
	}
	if err := s.renderAllContent(); err != nil {
		return err
	}
	s.prepareTemplateViews()
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
	if s.Config.GenerateSitemap && !s.hasStaticFile("sitemap.xml") {
		if err := s.renderSitemap(); err != nil {
			return err
		}
	}
	if s.Config.GenerateFeeds {
		if err := s.renderFeed(); err != nil {
			return err
		}
	}
	if !s.hasStaticFile("404.html") {
		if err := s.render404(opts.LiveReloadURL); err != nil {
			return err
		}
	}
	if s.Config.GenerateRobotsTXT && !s.hasStaticFile("robots.txt") {
		if err := s.renderRobots(); err != nil {
			return err
		}
	}

	if (s.Config.BuildSearchIndex || s.Config.Search.BuildIndex) && !s.hasStaticFile(s.Config.Search.IndexPath) {
		if err := search.BuildIndexFS(s.Library, s.OutputFS, s.Config.Search.IndexPath); err != nil {
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

func (s *Site) CheckExternalLinks() ([]linkcheck.Result, error) {
	results, err := s.CheckLinks()
	return lo.Filter(results, func(r linkcheck.Result, _ int) bool { return !r.Internal }), err
}

func (s *Site) CheckLinks() ([]linkcheck.Result, error) {
	return linkcheck.CheckOutput(s.OutputFS, s.Config.BaseURL, s.Config.LinkChecker)
}

type pageRenderArtifact struct {
	Source string
	Path   string
	HTML   string
	Page   *content.Page
	Err    error
}

// Render the complete graph before any output template can inspect other content.
func (s *Site) renderAllContent() error {
	defs := s.Templates.ShortcodeDefinitions()
	paths := lo.Keys(s.Library.Pages)
	sort.Strings(paths)
	for _, rel := range paths {
		pg := s.Library.Pages[rel]
		rendered, err := s.renderMarkdownWithShortcodes(pg, defs)
		if err != nil {
			return fmt.Errorf("render markdown %s: %w", rel, err)
		}
		pg.Content = rendered.Body
		pg.Summary = rendered.Summary
		pg.ExternalLinks = rendered.ExternalLinks
		pg.TOC = lo.Map(rendered.TOC, func(h markdown.Heading, _ int) content.Heading {
			return content.Heading{ID: h.ID, Level: h.Level, Title: h.Title}
		})
	}
	sectionPaths := lo.Keys(s.Library.Sections)
	sort.Strings(sectionPaths)
	for _, rel := range sectionPaths {
		sec := s.Library.Sections[rel]
		rendered, err := s.renderContentWithShortcodes(sec.RawContent, s.sectionView(rel, sec, []map[string]any{}), "section", sec.RelativePath, sec.Permalink, s.sectionAnchorLinksEnabled(sec), defs)
		if err != nil {
			return fmt.Errorf("render section markdown %q: %w", rel, err)
		}
		sec.Content = rendered.Body
	}
	return nil
}

func (s *Site) renderAllPages(liveReloadURL string, concurrency int) error {
	paths := lo.Keys(s.Library.Pages)
	sort.Strings(paths)
	if concurrency == 0 {
		concurrency = runtime.GOMAXPROCS(0)
	}
	workers := pool.NewWithResults[pageRenderArtifact]().WithMaxGoroutines(concurrency)
	for _, rel := range paths {
		if pg := s.Library.Pages[rel]; pg.Meta.Render != nil && !*pg.Meta.Render {
			continue
		}
		workers.Go(func() pageRenderArtifact {
			pg := s.Library.Pages[rel]
			outPath := filepath.Join(strings.TrimPrefix(pg.Path, "/"), "index.html")
			if strings.TrimSpace(pg.Meta.RedirectTo) != "" {
				redirect, err := s.renderRedirect(s.redirectTargetURL(pg.Meta.RedirectTo))
				if err != nil {
					err = fmt.Errorf("render page redirect %q to %q: %w", rel, outPath, err)
				}
				return pageRenderArtifact{Source: rel, Path: outPath, HTML: injectLiveReload(redirect, liveReloadURL), Page: pg, Err: err}
			}

			tplName := s.pageTemplateFor(pg)
			ctx := s.baseTemplateContext()
			ctx["page"] = s.templateViews.pages[rel]
			if sec, ok := s.templateViews.sections[pg.ParentSection]; ok {
				ctx["section"] = sec
			}
			ctx["current_url"] = pg.Permalink
			ctx["current_path"] = pg.Path
			html, err := s.Templates.Render(tplName, ctx)
			if err != nil {
				err = fmt.Errorf("render page %q using %q to %q: %w", rel, tplName, outPath, err)
			}
			html = injectLiveReload(html, liveReloadURL)
			return pageRenderArtifact{Source: rel, Path: outPath, HTML: html, Page: pg, Err: err}
		})
	}
	renderedArtifacts := workers.Wait()
	sort.Slice(renderedArtifacts, func(i, j int) bool { return renderedArtifacts[i].Source < renderedArtifacts[j].Source })
	for _, artifact := range renderedArtifacts {
		if artifact.Err != nil {
			return artifact.Err
		}
	}

	for _, artifact := range renderedArtifacts {
		if err := s.writeOutput(artifact.Path, artifact.HTML); err != nil {
			return err
		}
	}
	return nil
}

func (s *Site) baseTemplateContext() map[string]any {
	if s.templateViews == nil {
		s.prepareTemplateViews()
	}
	ctx := make(map[string]any, len(s.templateViews.base)+4)
	for key, val := range s.templateViews.base {
		ctx[key] = val
	}
	return ctx
}

func (s *Site) pageView(rel string, pg *content.Page) map[string]any {
	if s.templateViews != nil {
		if view, ok := s.templateViews.rawPages[rel]; ok {
			return view.(map[string]any)
		}
	}
	earlier, later := s.pageNeighbors(rel, pg)
	extra := pg.Meta.Extra
	if extra == nil {
		extra = map[string]any{}
	}
	taxonomies := map[string]any{}
	for kind, terms := range pg.Meta.Taxonomies {
		copied := make([]string, len(terms))
		copy(copied, terms)
		taxonomies[kind] = copied
	}
	var dateVal any
	if pg.Date != nil {
		dateVal = pg.Date.Format(time.RFC3339)
	}
	var updatedVal any
	if pg.Updated != nil {
		updatedVal = pg.Updated.Format(time.RFC3339)
	}
	slug := pg.Slug
	if strings.TrimSpace(pg.Meta.Slug) != "" {
		slug = strings.TrimSpace(pg.Meta.Slug)
	} else if slug == "" {
		trimmed := strings.Trim(pg.Path, "/")
		if trimmed != "" {
			parts := strings.Split(trimmed, "/")
			slug = parts[len(parts)-1]
		}
	}

	return map[string]any{
		"title":         pg.Meta.Title,
		"description":   pg.Meta.Description,
		"content":       pg.Content,
		"path":          pg.Path,
		"relative_path": pg.RelativePath,
		"permalink":     pg.Permalink,
		"toc":           s.tocView(pg.TOC),
		"summary":       pg.Summary,
		"slug":          slug,
		"date":          dateVal,
		"updated":       updatedVal,
		"extra":         extra,
		"earlier":       earlier,
		"later":         later,
		"assets": lo.Map(pg.Assets, func(asset string, _ int) string {
			return strings.TrimRight(pg.Permalink, "/") + "/" + pg.AssetRelPath(asset)
		}),
		"taxonomies": taxonomies,
		"aliases":    pg.Meta.Aliases,
		"draft":      pg.Meta.Draft,
	}
}

func (s *Site) sectionView(rel string, sec *content.Section, pages []map[string]any) map[string]any {
	extra := sec.Meta.Extra
	if extra == nil {
		extra = map[string]any{}
	}
	return map[string]any{
		"extra":             extra,
		"title":             sec.Meta.Title,
		"description":       sec.Meta.Description,
		"path":              sec.Path,
		"relative_path":     sec.RelativePath,
		"permalink":         sec.Permalink,
		"pages":             pages,
		"subsections":       sec.Subsections,
		"content":           sec.Content,
		"paginate_by":       sec.Meta.PaginateBy,
		"paginate_path":     sec.Meta.PaginatePath,
		"paginate_reversed": sec.Meta.PaginateReversed,
	}
}

func (s *Site) tocView(toc []content.Heading) []map[string]any {
	return lo.Map(toc, func(h content.Heading, _ int) map[string]any {
		return map[string]any{
			"id":    h.ID,
			"title": h.Title,
			"level": h.Level,
		}
	})
}

func (s *Site) pageNeighbors(rel string, pg *content.Page) (map[string]any, map[string]any) {
	_ = rel
	_ = pg
	return nil, nil
}

func (s *Site) renderMarkdownWithShortcodes(pg *content.Page, defs map[string]templates.ShortcodeDefinition) (markdown.Rendered, error) {
	return s.renderContentWithShortcodes(pg.RawContent, s.pageView(pg.RelativePath, pg), "page", pg.RelativePath, pg.Permalink, s.pageAnchorLinksEnabled(pg), defs)
}

func (s *Site) renderContentWithShortcodes(raw string, contentCtx map[string]any, contextKey, relativePath, permalink string, anchors bool, defs map[string]templates.ShortcodeDefinition) (markdown.Rendered, error) {
	out, scs, err := shortcode.Parse(raw)
	if err != nil {
		return markdown.Rendered{}, err
	}

	// Shortcodes see the same config and lookup helpers as page templates.
	shortcodeContext := func(sc shortcode.Shortcode) map[string]any {
		ctx := s.baseTemplateContext()
		ctx["nth"] = sc.Nth
		ctx[contextKey] = contentCtx
		for k, v := range sc.Args {
			ctx[k] = v
		}
		if sc.Body != nil {
			ctx["body"] = strings.TrimRight(*sc.Body, "\n")
		}
		return ctx
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
			return s.Templates.Engine.Render(def.Template, shortcodeContext(sc))
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
		Permalinks:                s.Library.Permalinks,
		CurrentPagePath:           relativePath,
		CurrentPagePermalink:      permalink,
		InsertAnchorLinks:         anchors,
		ExternalLinksTargetBlank:  s.Config.Markdown.ExternalLinksTargetBlank,
		HighlightTheme:            s.Config.Markdown.HighlightTheme,
		AllowMissingInternalLinks: s.Config.LinkChecker.InternalLevel == config.LinkCheckerWarn,
	})
	if err != nil {
		return markdown.Rendered{}, err
	}
	rendered.Body = shortcodeParagraphRe.ReplaceAllString(rendered.Body, shortcode.Placeholder)
	for _, sc := range htmlSCs {
		def, ok := defs[sc.Name]
		if !ok {
			return markdown.Rendered{}, fmt.Errorf("unknown shortcode: %s", sc.Name)
		}
		repl, rErr := s.Templates.Engine.Render(def.Template, shortcodeContext(sc))
		if rErr != nil {
			return markdown.Rendered{}, rErr
		}
		repl = strings.TrimRight(repl, "\n") + "\n"
		rendered.Body = strings.Replace(rendered.Body, shortcode.Placeholder, repl, 1)
	}
	rendered.Body = strings.ReplaceAll(rendered.Body, `<span id="continue-reading"></span>`+"\n<h", `<span id="continue-reading"></span><h`)
	if rendered.Summary != nil {
		before, _, _ := strings.Cut(rendered.Body, `<span id="continue-reading"></span>`)
		summary := before + `<span id="continue-reading"></span>`
		rendered.Summary = &summary
	}

	return rendered, nil
}

func (s *Site) renderSections(liveReloadURL string) error {
	paths := lo.Keys(s.Library.Sections)
	sort.Strings(paths)
	for _, rel := range paths {
		sec := s.Library.Sections[rel]
		if sec.Meta.Render != nil && !*sec.Meta.Render {
			continue
		}
		tpl := "section.html"
		if isRootSectionPath(sec.RelativePath) && s.templateExists("index.html") {
			tpl = "index.html"
		}
		if sec.Meta.Template != "" {
			tpl = sec.Meta.Template
		}
		entries := s.sectionPageEntries(sec)
		for _, plan := range s.sectionRenderPlans(sec, entries) {
			if strings.TrimSpace(sec.Meta.RedirectTo) != "" {
				if plan.OutputPath == filepath.Join(strings.TrimPrefix(sec.Path, "/"), "index.html") {
					redirect, err := s.renderRedirect(s.redirectTargetURL(sec.Meta.RedirectTo))
					if err != nil {
						return fmt.Errorf("render section redirect %q to %q: %w", rel, plan.OutputPath, err)
					}
					if err := s.writeOutput(plan.OutputPath, injectLiveReload(redirect, liveReloadURL)); err != nil {
						return err
					}
				}
				continue
			}

			ctx := s.baseTemplateContext()
			ctx["current_url"] = sectionPagerPermalink(sec.Path, sec.Permalink, strings.Trim(sec.Meta.PaginatePath, "/"), 1)
			ctx["current_path"] = sec.Path
			ctx["section"] = s.sectionView(rel, sec, plan.Pages)
			slug := strings.Trim(sec.Path, "/")
			if strings.Contains(slug, "/") {
				parts := strings.Split(slug, "/")
				slug = parts[len(parts)-1]
			}
			ctx["page"] = map[string]any{
				"title":       sec.Meta.Title,
				"slug":        slug,
				"description": sec.Meta.Description,
				"extra":       map[string]any{},
			}
			if plan.Paginator != nil {
				ctx["paginator"] = plan.Paginator
				if cur, ok := plan.Paginator["current"].(string); ok {
					ctx["current_url"] = cur
					if strings.HasPrefix(cur, strings.TrimRight(s.Config.BaseURL, "/")) {
						ctx["current_path"] = strings.TrimPrefix(cur, strings.TrimRight(s.Config.BaseURL, "/"))
					}
				}
			}
			html, err := s.Templates.Render(tpl, ctx)
			if err != nil {
				return fmt.Errorf("render section %q using %q to %q: %w", rel, tpl, plan.OutputPath, err)
			}
			html = injectLiveReload(html, liveReloadURL)
			if err := s.writeOutput(plan.OutputPath, html); err != nil {
				return err
			}
		}
		if sec.Meta.PaginateBy > 0 {
			paginatePath := strings.Trim(sec.Meta.PaginatePath, "/")
			if paginatePath == "" {
				paginatePath = "page"
			}
			redirect, err := s.renderRedirect(sec.Permalink)
			if err != nil {
				return fmt.Errorf("render pagination alias for section %q: %w", rel, err)
			}
			if err := s.writeOutput(filepath.Join(strings.TrimPrefix(sec.Path, "/"), paginatePath, "1", "index.html"), redirect); err != nil {
				return err
			}
		}
	}

	if _, hasDefaultRoot := s.Library.Sections["_index.md"]; !hasDefaultRoot && s.templateExists("index.html") {
		ctx := s.baseTemplateContext()
		ctx["section"] = map[string]any{
			"title":             "",
			"description":       "",
			"path":              "/",
			"relative_path":     "_index.md",
			"extra":             map[string]any{},
			"permalink":         strings.TrimRight(s.Config.BaseURL, "/") + "/",
			"pages":             []map[string]any{},
			"subsections":       []string{},
			"content":           "",
			"paginate_by":       0,
			"paginate_path":     "",
			"paginate_reversed": false,
		}
		ctx["page"] = map[string]any{
			"title":       s.Config.Title,
			"slug":        "",
			"description": "",
			"extra":       map[string]any{},
		}
		html, err := s.Templates.Render("index.html", ctx)
		if err != nil {
			return fmt.Errorf("render homepage using %q to %q: %w", "index.html", "index.html", err)
		}
		html = injectLiveReload(html, liveReloadURL)
		if err := s.writeOutput("index.html", html); err != nil {
			return err
		}
	}
	return nil
}

func isRootSectionPath(rel string) bool {
	d := filepath.ToSlash(filepath.Dir(rel))
	return d == "." || d == ""
}

func (s *Site) templateExists(name string) bool {
	_, err := s.Templates.Resolver.Resolve(name, s.Templates.Available)
	return err == nil
}

func (s *Site) anchorLinksEnabled(value string, fallback bool) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return fallback
	}
	switch v {
	case "none", "false", "off", "0":
		return false
	default:
		return true
	}
}

func (s *Site) sectionAnchorLinksEnabled(sec *content.Section) bool {
	return s.anchorLinksEnabled(sec.Meta.InsertAnchorLinks, s.Config.Markdown.InsertAnchorLinks)
}

func (s *Site) pageAnchorLinksEnabled(pg *content.Page) bool {
	base := s.Config.Markdown.InsertAnchorLinks
	if sec, ok := s.Library.Sections[pg.ParentSection]; ok {
		base = s.anchorLinksEnabled(sec.Meta.InsertAnchorLinks, base)
	}
	return s.anchorLinksEnabled(pg.Meta.InsertAnchorLinks, base)
}

func sectionParentRel(sectionRelPath string) string {
	dir := filepath.ToSlash(filepath.Dir(sectionRelPath))
	if dir == "." || dir == "" {
		return "_index.md"
	}
	parentDir := filepath.ToSlash(filepath.Dir(dir))
	if parentDir == "." {
		parentDir = ""
	}
	if parentDir == "" {
		return "_index.md"
	}
	return parentDir + "/_index.md"
}

func (s *Site) pageTemplateFor(pg *content.Page) string {
	if pg.Meta.Template != "" {
		return pg.Meta.Template
	}
	for secRel := pg.ParentSection; secRel != ""; {
		sec, ok := s.Library.Sections[secRel]
		if ok && sec.Meta.PageTemplate != "" {
			return sec.Meta.PageTemplate
		}
		next := sectionParentRel(secRel)
		if next == secRel {
			break
		}
		secRel = next
	}
	return "page.html"
}

func (s *Site) redirectTargetURL(target string) string {
	t := strings.TrimSpace(target)
	if strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") {
		return t
	}
	p := "/" + strings.Trim(t, "/") + "/"
	return strings.TrimRight(s.Config.BaseURL, "/") + p
}

func (s *Site) renderRedirect(url string) (string, error) {
	return s.Templates.Render("__zola_builtins/internal/alias.html", map[string]any{"url": url})
}

func (s *Site) renderFirstTemplate(candidates []string, ctx map[string]any) (string, error) {
	for _, name := range candidates {
		if !s.templateExists(name) {
			continue
		}
		return s.Templates.Render(name, ctx)
	}
	return "", fmt.Errorf("no matching template among %q", candidates)
}

type sectionRenderPlan struct {
	OutputPath string
	Pages      []map[string]any
	Paginator  map[string]any
}

func (s *Site) sectionPageEntries(sec *content.Section) []map[string]any {
	if s.templateViews != nil {
		if entries, ok := s.templateViews.sectionPages[sec]; ok {
			return entries
		}
	}
	type pageEntry struct {
		rel string
		pg  *content.Page
	}
	entries := make([]pageEntry, 0, len(sec.Pages))
	for _, p := range sec.Pages {
		pg := s.Library.Pages[p]
		if pg == nil {
			continue
		}
		if pg.Meta.Render != nil && !*pg.Meta.Render {
			continue
		}
		entries = append(entries, pageEntry{rel: p, pg: pg})
	}

	// sec.Pages is already ordered by the content loader.
	return lo.Map(entries, func(entry pageEntry, _ int) map[string]any {
		return s.pageView(entry.rel, entry.pg)
	})
}

func (s *Site) sectionRenderPlans(sec *content.Section, entries []map[string]any) []sectionRenderPlan {
	baseRel := strings.TrimPrefix(sec.Path, "/")
	out := []sectionRenderPlan{{
		OutputPath: filepath.Join(baseRel, "index.html"),
		Pages:      entries,
	}}

	perPage := sec.Meta.PaginateBy
	if perPage <= 0 {
		return out
	}

	reordered := make([]map[string]any, 0, len(entries))
	if sec.Meta.PaginateReversed {
		for i := len(entries) - 1; i >= 0; i-- {
			reordered = append(reordered, entries[i])
		}
	} else {
		reordered = append(reordered, entries...)
	}

	pagesCount := len(reordered)
	numberPagers := 1
	if pagesCount > 0 {
		numberPagers = (pagesCount + perPage - 1) / perPage
	}
	paginatePath := strings.Trim(sec.Meta.PaginatePath, "/")
	if paginatePath == "" {
		paginatePath = "page"
	}
	plans := make([]sectionRenderPlan, 0, numberPagers)
	baseURL := strings.TrimSuffix(sec.Permalink, "/")
	if baseURL == "" {
		baseURL = "/"
	}

	for i := 1; i <= numberPagers; i++ {
		start := (i - 1) * perPage
		end := start + perPage
		if end > pagesCount {
			end = pagesCount
		}
		chunk := reordered[start:end]
		outputPath, currentURL := sectionPagerLocation(sec.Path, sec.Permalink, paginatePath, i)
		pager := map[string]any{
			"base_url":      baseURL,
			"current_index": i,
			"number_pagers": numberPagers,
			"per_page":      perPage,
			"paginate_by":   perPage,
			"total_pages":   pagesCount,
			"first":         sectionPagerPermalink(sec.Path, sec.Permalink, paginatePath, 1),
			"last":          sectionPagerPermalink(sec.Path, sec.Permalink, paginatePath, numberPagers),
			"current":       currentURL,
			"pages":         chunk,
		}
		if i > 1 {
			pager["previous"] = sectionPagerPermalink(sec.Path, sec.Permalink, paginatePath, i-1)
		}
		if i < numberPagers {
			pager["next"] = sectionPagerPermalink(sec.Path, sec.Permalink, paginatePath, i+1)
		}
		plans = append(plans, sectionRenderPlan{OutputPath: outputPath, Pages: chunk, Paginator: pager})
	}

	return plans
}

func sectionPagerLocation(sectionPath, sectionPermalink, paginatePath string, index int) (string, string) {
	if index <= 1 {
		return filepath.Join(strings.TrimPrefix(sectionPath, "/"), "index.html"), sectionPermalink
	}
	rel := filepath.Join(strings.TrimPrefix(sectionPath, "/"), paginatePath, strconv.Itoa(index), "index.html")
	return rel, sectionPagerPermalink(sectionPath, sectionPermalink, paginatePath, index)
}

func sectionPagerPermalink(sectionPath, sectionPermalink, paginatePath string, index int) string {
	if index <= 1 {
		return sectionPermalink
	}
	base := strings.TrimSuffix(sectionPermalink, "/")
	if base == "" {
		base = "/"
	}
	if base == "/" {
		return "/" + strings.Trim(filepath.ToSlash(filepath.Join(paginatePath, strconv.Itoa(index))), "/") + "/"
	}
	return base + "/" + strings.Trim(filepath.ToSlash(filepath.Join(paginatePath, strconv.Itoa(index))), "/") + "/"
}

func (s *Site) renderTaxonomies(liveReloadURL string) error {
	for _, tax := range s.Library.Taxonomies {
		taxNameSlug := cmp.Or(slug.Normalize(tax.Name), "item")
		taxListPath := "/" + taxNameSlug + "/"
		termItems := make([]map[string]any, 0, len(tax.Terms))
		for termName, term := range tax.Terms {
			pathSlug := cmp.Or(slug.Normalize(termName), "item")
			taxPath := taxListPath + pathSlug + "/"
			entries := make([]map[string]any, 0, len(term.Pages))
			for _, rel := range term.Pages {
				pg := s.Library.Pages[rel]
				if pg == nil {
					continue
				}
				entries = append(entries, s.pageView(rel, pg))
			}
			termItems = append(termItems, map[string]any{
				"name":      termName,
				"slug":      pathSlug,
				"path":      taxPath,
				"permalink": strings.TrimRight(s.Config.BaseURL, "/") + taxPath,
				"pages":     entries,
				"count":     len(entries),
			})
		}
		sort.SliceStable(termItems, func(i, j int) bool {
			return termItems[i]["name"].(string) > termItems[j]["name"].(string)
		})

		ctxList := s.baseTemplateContext()
		ctxList["current_path"] = taxListPath
		ctxList["current_url"] = strings.TrimRight(s.Config.BaseURL, "/") + taxListPath
		ctxList["taxonomy"] = map[string]any{"name": tax.Name, "terms": termItems}
		ctxList["terms"] = termItems
		listHTML, listErr := s.renderFirstTemplate([]string{
			tax.Name + "/list.html",
			taxNameSlug + "/list.html",
			"taxonomy_list.html",
		}, ctxList)
		if listErr != nil {
			return fmt.Errorf("render taxonomy %q list to %q: %w", tax.Name, taxListPath+"index.html", listErr)
		}
		listHTML = injectLiveReload(listHTML, liveReloadURL)
		if err := s.writeOutput(filepath.Join(strings.TrimPrefix(taxListPath, "/"), "index.html"), listHTML); err != nil {
			return err
		}

		for termName, term := range tax.Terms {
			pathSlug := cmp.Or(slug.Normalize(termName), "item")
			taxPath := taxListPath + pathSlug + "/"
			entries := make([]map[string]any, 0, len(term.Pages))
			for _, rel := range term.Pages {
				pg := s.Library.Pages[rel]
				if pg == nil {
					continue
				}
				entries = append(entries, s.pageView(rel, pg))
			}
			ctx := s.baseTemplateContext()
			ctx["current_path"] = taxPath
			ctx["current_url"] = strings.TrimRight(s.Config.BaseURL, "/") + taxPath
			termObj := map[string]any{"name": termName, "slug": pathSlug, "pages": entries, "path": taxPath, "permalink": strings.TrimRight(s.Config.BaseURL, "/") + taxPath}
			ctx["taxonomy"] = map[string]any{"name": tax.Name, "term": termName, "pages": entries}
			ctx["term"] = termObj
			html, err := s.renderFirstTemplate([]string{
				tax.Name + "/single.html",
				taxNameSlug + "/single.html",
				"taxonomy_single.html",
			}, ctx)
			if err != nil {
				return fmt.Errorf("render taxonomy %q term %q to %q: %w", tax.Name, termName, taxPath+"index.html", err)
			}
			html = injectLiveReload(html, liveReloadURL)
			if err := s.writeOutput(filepath.Join(strings.TrimPrefix(taxPath, "/"), "index.html"), html); err != nil {
				return err
			}
			if s.taxonomyFeedEnabled(tax.Name) {
				taxPages := make([]*content.Page, 0)
				if tx, ok := s.Library.Taxonomies[tax.Name]; ok {
					if tt, ok := tx.Terms[termName]; ok {
						for _, rel := range tt.Pages {
							if p := s.Library.Pages[rel]; p != nil {
								taxPages = append(taxPages, p)
							}
						}
					}
				}
				for _, feedName := range s.feedFilenames() {
					feed, err := s.feedXML(feedName, strings.TrimRight(s.Config.BaseURL, "/")+taxPath+feedName, strings.TrimRight(s.Config.BaseURL, "/"), s.Config.Title+" - "+termName, taxPages, map[string]any{"term": termObj, "taxonomy": map[string]any{"name": tax.Name}})
					if err != nil {
						return err
					}
					if err := s.writeOutput(filepath.Join(strings.TrimPrefix(taxPath, "/"), feedName), feed); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (s *Site) renderSitemap() error {
	urls, lastmods := s.sitemapEntries()
	if !s.hasUserTemplate("sitemap.xml") {
		return s.writeOutput("sitemap.xml", defaultSitemapXML(urls, lastmods))
	}
	entries := lo.Map(urls, func(u string, _ int) map[string]any {
		entry := map[string]any{"permalink": u}
		if lm := lastmods[u]; lm != "" {
			entry["updated"] = lm
		}
		return entry
	})
	ctx := s.baseTemplateContext()
	ctx["entries"] = entries
	out, err := s.Templates.Render("sitemap.xml", ctx)
	if err != nil {
		return fmt.Errorf("render sitemap.xml: %w", err)
	}
	return s.writeOutput("sitemap.xml", out)
}

// hasUserTemplate reports whether the site or its theme provides name, as
// opposed to the built-in fallbacks.
func (s *Site) hasUserTemplate(name string) bool {
	resolved, err := s.Templates.Resolver.Resolve(name, s.Templates.Available)
	return err == nil && !strings.HasPrefix(resolved, "__zola_builtins/")
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return s
	}
	return b.String()
}

func xmlEscapeHTMLPayload(s string) string {
	escaped := xmlEscape(s)
	escaped = strings.ReplaceAll(escaped, "&#39;", "&#x27;")
	escaped = strings.ReplaceAll(escaped, "&lt;/", "&lt;&#x2F;")
	return escaped
}

func formatAtomTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0 {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Format("2006-01-02T15:04:05+00:00")
	}
	return t.UTC().Format("2006-01-02T15:04:05+00:00")
}

func (s *Site) sortedPagesForFeed(pages []*content.Page) []*content.Page {
	out := lo.Filter(pages, func(p *content.Page, _ int) bool {
		return p != nil
	})
	sort.SliceStable(out, func(i, j int) bool {
		di := out[i].Date
		dj := out[j].Date
		if di != nil && dj != nil && !di.Equal(*dj) {
			return di.After(*dj)
		}
		if di != nil && dj == nil {
			return true
		}
		if di == nil && dj != nil {
			return false
		}
		ti := strings.TrimSpace(out[i].Meta.Title)
		tj := strings.TrimSpace(out[j].Meta.Title)
		if (ti == "") != (tj == "") {
			return ti == ""
		}
		return out[i].RelativePath < out[j].RelativePath
	})
	return out
}

func (s *Site) defaultAtomXML(feedURL string, htmlURL string, title string, pages []*content.Page) string {
	ordered := s.sortedPagesForFeed(pages)
	updated := ""
	if len(ordered) > 0 {
		updated = formatAtomTime(ordered[0].Date)
	}
	if updated == "" {
		// Keep the output reproducible: no wall-clock time in generated files.
		updated = time.Unix(0, 0).UTC().Format("2006-01-02T15:04:05+00:00")
	}

	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<feed xmlns=\"http://www.w3.org/2005/Atom\">\n")
	b.WriteString("    <title>")
	b.WriteString(xmlEscape(title))
	b.WriteString("</title>\n")
	b.WriteString("    <link rel=\"self\" type=\"application/atom+xml\" href=\"")
	b.WriteString(xmlEscape(feedURL))
	b.WriteString("\"/>\n")
	b.WriteString("    <link rel=\"alternate\" type=\"text/html\" href=\"")
	b.WriteString(xmlEscape(htmlURL))
	b.WriteString("\"/>\n")
	b.WriteString("    <generator uri=\"https://www.getzola.org/\">Zola</generator>\n")
	b.WriteString("    <updated>")
	b.WriteString(updated)
	b.WriteString("</updated>\n")
	b.WriteString("    <id>")
	b.WriteString(xmlEscape(feedURL))
	b.WriteString("</id>\n")

	for _, p := range ordered {
		if p.Date == nil {
			continue
		}
		b.WriteString("    <entry>\n")
		b.WriteString("        <title>")
		b.WriteString(xmlEscape(p.Meta.Title))
		b.WriteString("</title>\n")
		if p.Date != nil {
			ts := formatAtomTime(p.Date)
			b.WriteString("        <published>")
			b.WriteString(ts)
			b.WriteString("</published>\n")
			b.WriteString("        <updated>")
			b.WriteString(ts)
			b.WriteString("</updated>\n")
		}

		authors := p.Meta.Authors
		if len(authors) == 0 {
			if strings.TrimSpace(s.Config.Author) != "" {
				authors = []string{s.Config.Author}
			} else {
				authors = []string{"Unknown"}
			}
		}
		for _, author := range authors {
			b.WriteString("        <author><name>")
			b.WriteString(xmlEscape(author))
			b.WriteString("</name></author>\n")
		}

		b.WriteString("        <link rel=\"alternate\" type=\"text/html\" href=\"")
		b.WriteString(xmlEscape(p.Permalink))
		b.WriteString("\"/>\n")
		b.WriteString("        <id>")
		b.WriteString(xmlEscape(p.Permalink))
		b.WriteString("</id>\n")

		if p.Summary != nil {
			summary := continueReadingMarkerRe.ReplaceAllString(*p.Summary, "")
			b.WriteString("        <summary type=\"html\">")
			b.WriteString(xmlEscapeHTMLPayload(summary))
			b.WriteString("</summary>\n")
		} else {
			b.WriteString("        <content type=\"html\" xml:base=\"")
			b.WriteString(xmlEscape(p.Permalink))
			b.WriteString("\">")
			b.WriteString(xmlEscapeHTMLPayload(p.Content))
			b.WriteString("</content>\n")
		}

		b.WriteString("    </entry>\n")
	}
	b.WriteString("</feed>")
	return b.String()
}

// feedXML renders one feed. A site or theme template named after the feed file
// (atom.xml, rss.xml, ...) wins; otherwise names ending in rss.xml get RSS 2.0
// and everything else gets Atom.
func (s *Site) feedXML(name, feedURL, htmlURL, title string, pages []*content.Page, extra map[string]any) (string, error) {
	ordered := lo.Filter(s.sortedPagesForFeed(pages), func(p *content.Page, _ int) bool { return p.Date != nil })
	if limit := s.Config.FeedLimit; limit > 0 && len(ordered) > limit {
		ordered = ordered[:limit]
	}
	switch {
	case s.hasUserTemplate(name):
		ctx := s.baseTemplateContext()
		ctx["feed_url"] = feedURL
		ctx["last_updated"] = ""
		if len(ordered) > 0 {
			ctx["last_updated"] = formatAtomTime(ordered[0].Date)
		}
		ctx["pages"] = lo.Map(ordered, func(p *content.Page, _ int) map[string]any { return s.pageView(p.RelativePath, p) })
		for k, v := range extra {
			ctx[k] = v
		}
		out, err := s.Templates.Render(name, ctx)
		if err != nil {
			return "", fmt.Errorf("render feed %q: %w", name, err)
		}
		return out, nil
	case strings.HasSuffix(name, "rss.xml"):
		return s.defaultRSSXML(feedURL, htmlURL, title, ordered), nil
	default:
		return s.defaultAtomXML(feedURL, htmlURL, title, ordered), nil
	}
}

func (s *Site) defaultRSSXML(feedURL, htmlURL, title string, pages []*content.Page) string {
	description := cmp.Or(strings.TrimSpace(s.Config.Description), title)
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<rss version=\"2.0\" xmlns:atom=\"http://www.w3.org/2005/Atom\">\n    <channel>\n")
	b.WriteString("        <title>" + xmlEscape(title) + "</title>\n")
	b.WriteString("        <link>" + xmlEscape(htmlURL) + "</link>\n")
	b.WriteString("        <description>" + xmlEscape(description) + "</description>\n")
	b.WriteString("        <generator>Zola</generator>\n")
	b.WriteString("        <atom:link href=\"" + xmlEscape(feedURL) + "\" rel=\"self\" type=\"application/rss+xml\"/>\n")
	if len(pages) > 0 && pages[0].Date != nil {
		b.WriteString("        <lastBuildDate>" + pages[0].Date.UTC().Format(time.RFC1123Z) + "</lastBuildDate>\n")
	}
	for _, p := range pages {
		b.WriteString("        <item>\n")
		b.WriteString("            <title>" + xmlEscape(p.Meta.Title) + "</title>\n")
		b.WriteString("            <pubDate>" + p.Date.UTC().Format(time.RFC1123Z) + "</pubDate>\n")
		author := cmp.Or(strings.TrimSpace(s.Config.Author), "Unknown")
		if len(p.Meta.Authors) > 0 {
			author = p.Meta.Authors[0]
		}
		b.WriteString("            <author>" + xmlEscape(author) + "</author>\n")
		b.WriteString("            <link>" + xmlEscape(p.Permalink) + "</link>\n")
		b.WriteString("            <guid>" + xmlEscape(p.Permalink) + "</guid>\n")
		body := p.Content
		if p.Summary != nil {
			body = continueReadingMarkerRe.ReplaceAllString(*p.Summary, "")
		}
		b.WriteString("            <description>" + xmlEscapeHTMLPayload(body) + "</description>\n")
		b.WriteString("        </item>\n")
	}
	b.WriteString("    </channel>\n</rss>")
	return b.String()
}

// sitemapEntries returns every published URL in sorted order plus the lastmod
// of those that have one.
func (s *Site) sitemapEntries() ([]string, map[string]string) {
	lastmods := map[string]string{}
	urlsSet := map[string]struct{}{}
	urlsSet[strings.TrimRight(s.Config.BaseURL, "/")+"/"] = struct{}{}
	for _, p := range s.Library.Pages {
		if p.Meta.Render != nil && !*p.Meta.Render {
			continue
		}
		urlsSet[p.Permalink] = struct{}{}
		if p.Updated != nil {
			lastmods[p.Permalink] = s.pageSitemapLastmod(p.SourcePath, "updated", p.Meta.Updated, p.Updated)
		} else if p.Date != nil {
			lastmods[p.Permalink] = s.pageSitemapLastmod(p.SourcePath, "date", p.Meta.Date, p.Date)
		}
	}
	for _, sec := range s.Library.Sections {
		if sec.Meta.Render != nil && !*sec.Meta.Render {
			continue
		}
		urlsSet[sec.Permalink] = struct{}{}
		if sec.Meta.PaginateBy > 0 {
			n := len(s.sectionRenderPlans(sec, s.sectionPageEntries(sec)))
			if n > 0 {
				paginatePath := strings.Trim(sec.Meta.PaginatePath, "/")
				if paginatePath == "" {
					paginatePath = "page"
				}
				if n > 1 {
					base := strings.TrimRight(sec.Permalink, "/")
					if base == "" {
						base = "/"
					}
					if base == "/" {
						urlsSet[strings.TrimRight(s.Config.BaseURL, "/")+"/"+paginatePath+"/1/"] = struct{}{}
					} else {
						urlsSet[base+"/"+paginatePath+"/1/"] = struct{}{}
					}
				}
				for i := 1; i <= n; i++ {
					urlsSet[sectionPagerPermalink(sec.Path, sec.Permalink, paginatePath, i)] = struct{}{}
				}
			}
		}
	}
	for _, tx := range s.Library.Taxonomies {
		taxListPath := "/" + cmp.Or(slug.Normalize(tx.Name), "item") + "/"
		urlsSet[strings.TrimRight(s.Config.BaseURL, "/")+taxListPath] = struct{}{}
		for termName := range tx.Terms {
			termSlug := cmp.Or(slug.Normalize(termName), "item")
			urlsSet[strings.TrimRight(s.Config.BaseURL, "/")+taxListPath+termSlug+"/"] = struct{}{}
		}
	}

	urls := make([]string, 0, len(urlsSet))
	for u := range urlsSet {
		urls = append(urls, u)
	}

	sort.Strings(urls)
	return urls, lastmods
}

func defaultSitemapXML(urls []string, lastmods map[string]string) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n")
	for _, u := range urls {
		b.WriteString("    <url>\n")
		b.WriteString("        <loc>")
		b.WriteString(xmlEscape(u))
		b.WriteString("</loc>\n")
		if lm := lastmods[u]; lm != "" {
			b.WriteString("        <lastmod>")
			b.WriteString(lm)
			b.WriteString("</lastmod>\n")
		}
		b.WriteString("    </url>\n")
	}
	b.WriteString("</urlset>")
	return b.String()
}

func formatSitemapDate(raw any, parsed *time.Time) string {
	if parsed == nil {
		return ""
	}
	if s, ok := raw.(string); ok {
		t := strings.TrimSpace(s)
		if strings.Contains(t, "T") {
			return parsed.UTC().Format(time.RFC3339)
		}
	}
	return parsed.Format("2006-01-02")
}

func (s *Site) pageSitemapLastmod(sourcePath string, key string, raw any, parsed *time.Time) string {
	if parsed == nil {
		return ""
	}
	if lex, ok := s.extractFrontMatterScalar(sourcePath, key); ok {
		if strings.Contains(strings.ToLower(lex), "t") {
			return parsed.UTC().Format(time.RFC3339)
		}
		return parsed.Format("2006-01-02")
	}
	return formatSitemapDate(raw, parsed)
}

func (site *Site) extractFrontMatterScalar(sourcePath string, key string) (string, bool) {
	rel, err := filepath.Rel(site.BasePath, sourcePath)
	if err != nil {
		return "", false
	}
	b, err := site.Templates.SourceFS.ReadFile(filepath.ToSlash(rel))
	if err != nil {
		return "", false
	}
	s := strings.TrimPrefix(string(b), "\ufeff")
	if strings.HasPrefix(s, "---\n") {
		end := strings.Index(s[4:], "\n---")
		if end == -1 {
			return "", false
		}
		block := s[4 : 4+end]
		for _, line := range strings.Split(block, "\n") {
			trim := strings.TrimSpace(line)
			if !strings.HasPrefix(trim, key+":") {
				continue
			}
			v := strings.TrimSpace(strings.TrimPrefix(trim, key+":"))
			v = strings.Trim(v, `"'`)
			if v == "" {
				return "", false
			}
			return v, true
		}
		return "", false
	}
	if strings.HasPrefix(s, "+++\n") {
		end := strings.Index(s[4:], "\n+++")
		if end == -1 {
			return "", false
		}
		block := s[4 : 4+end]
		for _, line := range strings.Split(block, "\n") {
			trim := strings.TrimSpace(line)
			if !strings.HasPrefix(trim, key+" =") {
				continue
			}
			parts := strings.SplitN(trim, "=", 2)
			if len(parts) != 2 {
				continue
			}
			v := strings.TrimSpace(parts[1])
			v = strings.Trim(v, `"'`)
			if v == "" {
				return "", false
			}
			return v, true
		}
		return "", false
	}
	return "", false
}

func (s *Site) renderFeed() error {
	allPages := make([]*content.Page, 0, len(s.Library.Pages))
	for _, p := range s.Library.Pages {
		allPages = append(allPages, p)
	}
	for _, feedName := range s.feedFilenames() {
		feed, err := s.feedXML(feedName, strings.TrimRight(s.Config.BaseURL, "/")+"/"+feedName, strings.TrimRight(s.Config.BaseURL, "/"), s.Config.Title, allPages, nil)
		if err != nil {
			return err
		}
		if err := s.writeOutput(feedName, feed); err != nil {
			return err
		}
	}

	for _, sec := range s.Library.Sections {
		if !sec.Meta.GenerateFeed && !sec.Meta.GenerateFeeds {
			continue
		}
		secEntries := s.sectionPageEntries(sec)
		secPages := lo.FilterMap(secEntries, func(entry map[string]any, _ int) (*content.Page, bool) {
			rel, _ := entry["relative_path"].(string)
			p := s.Library.Pages[rel]
			return p, p != nil
		})
		for _, feedName := range s.feedFilenames() {
			secFeed, err := s.feedXML(feedName, strings.TrimRight(s.Config.BaseURL, "/")+sec.Path+feedName, strings.TrimRight(s.Config.BaseURL, "/")+sec.Path, s.Config.Title+" - "+sec.Meta.Title, secPages, map[string]any{"section": s.sectionView(sec.RelativePath, sec, secEntries)})
			if err != nil {
				return err
			}
			if err := s.writeOutput(filepath.Join(strings.TrimPrefix(sec.Path, "/"), feedName), secFeed); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Site) feedFilenames() []string {
	if len(s.Config.FeedFilenames) == 0 {
		return []string{"atom.xml"}
	}
	out := make([]string, 0, len(s.Config.FeedFilenames))
	for _, name := range s.Config.FeedFilenames {
		n := strings.TrimSpace(name)
		if n == "" {
			continue
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return []string{"atom.xml"}
	}
	return out
}

func (s *Site) taxonomyFeedEnabled(name string) bool {
	for _, tx := range s.Config.Taxonomies {
		if tx.Name == name {
			return tx.Feed
		}
	}
	return false
}

func (s *Site) render404(liveReloadURL string) error {
	content, err := s.Templates.Render("404.html", s.baseTemplateContext())
	if err != nil {
		return fmt.Errorf("render 404.html: %w", err)
	}
	content = injectLiveReload(content, liveReloadURL)
	return s.writeOutput("404.html", content)
}

func (s *Site) renderRobots() error {
	content, err := s.Templates.Render("robots.txt", s.baseTemplateContext())
	if err != nil {
		return fmt.Errorf("render robots.txt: %w", err)
	}
	return s.writeOutput("robots.txt", content)
}

func (s *Site) renderAliases() error {
	for rel, sec := range s.Library.Sections {
		for _, alias := range sec.Meta.Aliases {
			redirect, err := s.renderRedirect(sec.Permalink)
			if err != nil {
				return fmt.Errorf("render alias %q for section %q: %w", alias, rel, err)
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

	for rel, p := range s.Library.Pages {
		if p.Meta.Render != nil && !*p.Meta.Render {
			continue
		}
		for _, alias := range p.Meta.Aliases {
			redirect, err := s.renderRedirect(p.Permalink)
			if err != nil {
				return fmt.Errorf("render alias %q for page %q: %w", alias, rel, err)
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
	if s.OutputFS == nil {
		s.OutputFS = filesystem.NewDiskFS(s.OutputPath)
	}
	if s.Config.Theme != "" {
		themeStatic := filepath.ToSlash(filepath.Join("themes", s.Config.Theme, "static"))
		if err := assets.CopyDirectoryFS(s.Templates.SourceFS, themeStatic, s.OutputFS); err != nil {
			return err
		}
	}
	return assets.CopyDirectoryFS(s.Templates.SourceFS, "static", s.OutputFS)
}

func (s *Site) copyColocatedAssets() error {
	for _, p := range s.Library.Pages {
		for _, asset := range p.Assets {
			relName := p.AssetRelPath(asset)
			destDir := filepath.ToSlash(strings.TrimPrefix(p.Path, "/"))
			assetPath, err := filepath.Rel(s.BasePath, asset)
			if err != nil {
				return err
			}
			b, err := s.Templates.SourceFS.ReadFile(filepath.ToSlash(assetPath))
			if err != nil {
				return err
			}
			relOut := filepath.ToSlash(filepath.Join(destDir, relName))

			if err := s.OutputFS.WriteFile(relOut, b, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Site) writeOutput(rel string, content string) error {
	if err := filesystem.ValidatePath(filepath.ToSlash(rel)); err != nil {
		return fmt.Errorf("invalid output path %q: %w", rel, err)
	}
	lowerRel := strings.ToLower(rel)

	if strings.HasSuffix(lowerRel, ".html") {
		var err error
		content, err = s.injectHighlightStylesheetIfNeeded(rel, content)
		if err != nil {
			return err
		}
	}

	if s.Config.MinifyHTML && strings.HasSuffix(lowerRel, ".html") {
		content = minifyHTML(content)
	}
	content = strings.ReplaceAll(content, "&#x2f;", "&#x2F;")

	if strings.HasSuffix(lowerRel, ".html") || strings.HasSuffix(lowerRel, ".xml") || strings.HasSuffix(lowerRel, ".txt") || strings.HasSuffix(lowerRel, ".css") || strings.HasSuffix(lowerRel, ".js") {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
	}

	return s.OutputFS.WriteFile(filepath.ToSlash(rel), []byte(content), 0o644)
}

func (s *Site) injectHighlightStylesheetIfNeeded(rel string, content string) (string, error) {
	if s.highlightCSSPath == "" {
		return content, nil
	}
	if !strings.Contains(content, `data-highlighted="true"`) {
		return content, nil
	}
	href := relativeAssetHref(s.highlightCSSPath)
	updated := injectStylesheetIntoHead(content, href)
	if updated == content {
		return content, nil
	}
	if err := s.ensureHighlightStylesheet(); err != nil {
		return "", err
	}
	return updated, nil
}

func (s *Site) ensureHighlightStylesheet() error {
	if s.highlightCSSWritten || s.highlightCSSPath == "" {
		return nil
	}
	css, err := markdown.HighlightCSS(s.Config.Markdown.HighlightTheme)
	if err != nil {
		return err
	}
	s.highlightCSSWritten = true
	return s.writeOutput(s.highlightCSSPath, css)
}

func highlightStylesheetFilename(theme string) string {
	theme = strings.TrimSpace(strings.ToLower(theme))
	if theme == "" {
		theme = "highlight"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range theme {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if r == '-' || r == '_' {
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "highlight"
	}
	return "code-" + out + ".css"
}

func relativeAssetHref(assetRel string) string {
	assetRel = filepath.ToSlash(strings.TrimPrefix(assetRel, "/"))
	if assetRel == "" {
		return "/"
	}
	return "/" + assetRel
}

func injectStylesheetIntoHead(html string, href string) string {
	if href == "" {
		return html
	}
	if strings.Contains(html, `href="`+href+`"`) {
		return html
	}
	lower := strings.ToLower(html)
	idx := strings.Index(lower, "</head>")
	if idx == -1 {
		return html
	}
	link := `<link rel="stylesheet" href="` + href + `">`
	return html[:idx] + link + html[idx:]
}

func minifyHTML(in string) string {
	out, err := htmlMinifier.String("text/html", in)
	if err != nil {
		return in
	}
	return out
}

func injectLiveReload(html string, reloadURL string) string {
	if reloadURL == "" {
		return html
	}
	if u, err := url.Parse(reloadURL); err == nil {
		reloadURL = u.RequestURI()
	}
	reloadPath, _ := json.Marshal(reloadURL)
	script := `<script>(function(){var u=new URL(` + string(reloadPath) + `,window.location.href);u.protocol=location.protocol==='https:'?'wss:':'ws:';var ws=new WebSocket(u);ws.onmessage=function(){window.location.reload();};})();</script>`
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
