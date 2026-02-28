package site

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

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
var shortcodeParagraphRe = regexp.MustCompile(`(?s)<p>\s*` + regexp.QuoteMeta(shortcode.Placeholder) + `\s*</p>`)

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
		enabledLangs := map[string]bool{s.Config.DefaultLanguage: true}
		for lang, opts := range s.Config.Languages {
			if opts.BuildSearchIndex {
				enabledLangs[lang] = true
			}
		}
		if err := search.BuildIndexForLanguages(s.Library, s.OutputPath, s.Config.Search.IndexPath, enabledLangs); err != nil {
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

			if strings.TrimSpace(pg.Meta.RedirectTo) != "" {
				redirect := s.renderRedirect(s.redirectTargetURL(pg.Meta.RedirectTo))
				results <- pageRenderArtifact{Path: filepath.Join(strings.TrimPrefix(pg.Path, "/"), "index.html"), HTML: injectLiveReload(redirect, liveReloadURL), Page: pg}
				continue
			}

			tplName := s.pageTemplateFor(pg)
			ctx := s.baseTemplateContext(pg.Lang)
			ctx["page"] = s.pageView(rel, pg)
			if sec, ok := s.Library.Sections[pg.ParentSection]; ok {
				ctx["section"] = map[string]any{
					"title":         sec.Meta.Title,
					"description":   sec.Meta.Description,
					"path":          sec.Path,
					"relative_path": sec.RelativePath,
					"permalink":     sec.Permalink,
					"pages":         s.sectionPageEntries(sec),
					"subsections":   sec.Subsections,
					"content":       sec.Content,
				}
			}
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
		if pg.Meta.Render != nil && !*pg.Meta.Render {
			continue
		}
		outPath := filepath.Join(strings.TrimPrefix(pg.Path, "/"), "index.html")
		artifact := artifacts[outPath]
		if artifact.Path == "" {
			continue
		}
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
	earlier, later := s.pageNeighbors(rel, pg)

	return map[string]any{
		"title":         pg.Meta.Title,
		"description":   pg.Meta.Description,
		"content":       pg.Content,
		"path":          pg.Path,
		"relative_path": pg.RelativePath,
		"permalink":     pg.Permalink,
		"lang":          pg.Lang,
		"toc":           s.tocView(pg.TOC),
		"summary":       pg.Summary,
		"slug":          pg.Slug,
		"date":          pg.Date,
		"earlier":       earlier,
		"later":         later,
		"translations":  trans,
		"assets":        pg.Assets,
		"taxonomies":    pg.Meta.Taxonomies,
		"aliases":       pg.Meta.Aliases,
		"draft":         pg.Meta.Draft,
	}
}

func (s *Site) tocView(toc []content.Heading) string {
	_ = s
	if len(toc) == 0 {
		return "[]"
	}
	return "[[object]]"
}

func (s *Site) pageNeighbors(rel string, pg *content.Page) (map[string]any, map[string]any) {
	_ = rel
	_ = pg
	return nil, nil
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
		InsertAnchorLinks:    s.pageAnchorLinksEnabled(pg),
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
		if !strings.Contains(repl, "<") {
			repl = repl + "\n\n"
		}
		rendered.Body = strings.Replace(rendered.Body, shortcode.Placeholder, repl, 1)
	}
	rendered.Body = strings.ReplaceAll(rendered.Body, `<span id="continue-reading"></span>`+"\n<h", `<span id="continue-reading"></span><h`)

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
		if sec.Meta.Render != nil && !*sec.Meta.Render {
			continue
		}
		renderedSection, secErr := markdown.RenderContent(sec.RawContent, markdown.RenderContext{
			Permalinks:           s.Library.Permalinks,
			CurrentPagePath:      sec.RelativePath,
			CurrentPagePermalink: sec.Permalink,
			InsertAnchorLinks:    s.sectionAnchorLinksEnabled(sec),
		})
		if secErr == nil {
			sec.Content = renderedSection.Body
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
					redirect := s.renderRedirect(s.redirectTargetURL(sec.Meta.RedirectTo))
					if err := s.writeOutput(plan.OutputPath, injectLiveReload(redirect, liveReloadURL)); err != nil {
						return err
					}
				}
				continue
			}

			ctx := s.baseTemplateContext(sec.Lang)
			ctx["current_url"] = sectionPagerPermalink(sec.Path, sec.Permalink, strings.Trim(sec.Meta.PaginatePath, "/"), 1)
			ctx["current_path"] = sec.Path
			ctx["section"] = map[string]any{
				"title":             sec.Meta.Title,
				"description":       sec.Meta.Description,
				"path":              sec.Path,
				"relative_path":     sec.RelativePath,
				"permalink":         sec.Permalink,
				"pages":             plan.Pages,
				"subsections":       sec.Subsections,
				"content":           sec.Content,
				"paginate_by":       sec.Meta.PaginateBy,
				"paginate_path":     sec.Meta.PaginatePath,
				"paginate_reversed": sec.Meta.PaginateReversed,
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
				html = "<html><body><h1>" + sec.Meta.Title + "</h1></body></html>"
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
			redirect, err := s.Templates.Engine.Render("__zola_builtins/internal/alias.html", map[string]any{"url": sec.Permalink})
			if err != nil {
				redirect = "<meta http-equiv=\"refresh\" content=\"0; url=" + sec.Permalink + "\">"
			}
			if err := s.writeOutput(filepath.Join(strings.TrimPrefix(sec.Path, "/"), paginatePath, "1", "index.html"), redirect); err != nil {
				return err
			}
		}
	}

	defaultRoot, hasDefaultRoot := s.Library.Sections["_index.md"]
	if hasDefaultRoot {
		for lang := range s.Config.Languages {
			if lang == s.Config.DefaultLanguage {
				continue
			}
			if _, ok := s.Library.Sections["_index."+lang+".md"]; ok {
				continue
			}
			ctx := s.baseTemplateContext(lang)
			ctx["section"] = map[string]any{
				"title":             defaultRoot.Meta.Title,
				"description":       defaultRoot.Meta.Description,
				"path":              "/" + lang + "/",
				"relative_path":     "_index." + lang + ".md",
				"permalink":         strings.TrimRight(s.Config.BaseURL, "/") + "/" + lang + "/",
				"pages":             []map[string]any{},
				"subsections":       []string{},
				"content":           defaultRoot.Content,
				"paginate_by":       0,
				"paginate_path":     "",
				"paginate_reversed": false,
			}
			rootTpl := "section.html"
			if s.templateExists("index.html") {
				rootTpl = "index.html"
			}
			html, err := s.Templates.Render(rootTpl, ctx)
			if err != nil {
				html = "<html><body><h1>" + defaultRoot.Meta.Title + "</h1></body></html>"
			}
			html = injectLiveReload(html, liveReloadURL)
			if err := s.writeOutput(filepath.Join(lang, "index.html"), html); err != nil {
				return err
			}
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
	lang := ""
	parts := strings.Split(filepath.Base(sectionRelPath), ".")
	if len(parts) >= 3 {
		lang = parts[len(parts)-2]
	}
	dir := filepath.ToSlash(filepath.Dir(sectionRelPath))
	if dir == "." || dir == "" {
		if lang == "" {
			return "_index.md"
		}
		return "_index." + lang + ".md"
	}
	parentDir := filepath.ToSlash(filepath.Dir(dir))
	if parentDir == "." {
		parentDir = ""
	}
	if parentDir == "" {
		if lang == "" {
			return "_index.md"
		}
		return "_index." + lang + ".md"
	}
	if lang == "" {
		return parentDir + "/_index.md"
	}
	return parentDir + "/_index." + lang + ".md"
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

func (s *Site) renderRedirect(url string) string {
	redirect, err := s.Templates.Engine.Render("__zola_builtins/internal/alias.html", map[string]any{"url": url})
	if err == nil {
		return redirect
	}
	return "<meta http-equiv=\"refresh\" content=\"0; url=" + url + "\">"
}

func (s *Site) renderFirstTemplate(candidates []string, ctx map[string]any) (string, error) {
	for _, name := range candidates {
		if !s.templateExists(name) {
			continue
		}
		out, err := s.Templates.Render(name, ctx)
		if err == nil {
			return out, nil
		}
	}
	return "", fmt.Errorf("no matching template")
}

type sectionRenderPlan struct {
	OutputPath string
	Pages      []map[string]any
	Paginator  map[string]any
}

func (s *Site) sectionPageEntries(sec *content.Section) []map[string]any {
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

	switch strings.ToLower(strings.TrimSpace(sec.Meta.SortBy)) {
	case "date":
		sort.SliceStable(entries, func(i, j int) bool {
			di := entries[i].pg.Date
			dj := entries[j].pg.Date
			if di != nil && dj != nil && !di.Equal(*dj) {
				return di.After(*dj)
			}
			if di != nil && dj == nil {
				return true
			}
			if di == nil && dj != nil {
				return false
			}
			if entries[i].pg.Meta.Weight != entries[j].pg.Meta.Weight {
				return entries[i].pg.Meta.Weight < entries[j].pg.Meta.Weight
			}
			if entries[i].pg.Meta.Title != entries[j].pg.Meta.Title {
				return entries[i].pg.Meta.Title < entries[j].pg.Meta.Title
			}
			return entries[i].rel < entries[j].rel
		})
	case "weight":
		sort.SliceStable(entries, func(i, j int) bool {
			wi := entries[i].pg.Meta.Weight
			wj := entries[j].pg.Meta.Weight
			if wi != wj {
				return wi < wj
			}
			if entries[i].pg.Date != nil && entries[j].pg.Date != nil && !entries[i].pg.Date.Equal(*entries[j].pg.Date) {
				return entries[i].pg.Date.After(*entries[j].pg.Date)
			}
			return entries[i].rel < entries[j].rel
		})
	default:
		// preserve content loader order for sections without explicit sorting
	}

	pages := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		pages = append(pages, s.pageView(entry.rel, entry.pg))
	}
	return pages
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
		termsByLang := map[string]map[string][]map[string]any{}
		for termName, term := range tax.Terms {
			for _, rel := range term.Pages {
				pg := s.Library.Pages[rel]
				if pg == nil {
					continue
				}
				if termsByLang[pg.Lang] == nil {
					termsByLang[pg.Lang] = map[string][]map[string]any{}
				}
				termsByLang[pg.Lang][termName] = append(termsByLang[pg.Lang][termName], s.pageView(rel, pg))
			}
		}

		for lang, termsMap := range termsByLang {
			termItems := make([]map[string]any, 0, len(termsMap))
			for termName, entries := range termsMap {
				pathSlug := slugifyURLSegment(termName)
				termItems = append(termItems, map[string]any{
					"name":      termName,
					"slug":      pathSlug,
					"path":      taxonomyPathForLang(lang, s.Config.DefaultLanguage, tax.Name, pathSlug),
					"permalink": strings.TrimRight(s.Config.BaseURL, "/") + taxonomyPathForLang(lang, s.Config.DefaultLanguage, tax.Name, pathSlug),
					"pages":     entries,
					"count":     len(entries),
				})
			}
			sort.SliceStable(termItems, func(i, j int) bool {
				return termItems[i]["name"].(string) > termItems[j]["name"].(string)
			})

			ctxList := s.baseTemplateContext(lang)
			ctxList["current_path"] = taxonomyPathForLang(lang, s.Config.DefaultLanguage, tax.Name, "")
			ctxList["current_url"] = strings.TrimRight(s.Config.BaseURL, "/") + taxonomyPathForLang(lang, s.Config.DefaultLanguage, tax.Name, "")
			ctxList["taxonomy"] = map[string]any{"name": tax.Name, "terms": termItems}
			ctxList["terms"] = termItems
			listHTML, listErr := s.renderFirstTemplate([]string{
				tax.Name + "/list.html",
				slugifyURLSegment(tax.Name) + "/list.html",
				"taxonomy_list.html",
			}, ctxList)
			if listErr != nil {
				var b strings.Builder
				b.WriteString("\n")
				for _, term := range termItems {
					b.WriteString("    ")
					b.WriteString(term["name"].(string))
					b.WriteString("  ")
					b.WriteString(term["slug"].(string))
					b.WriteString(" ")
					b.WriteString(strconv.Itoa(term["count"].(int)))
					b.WriteString("\n")
				}
				b.WriteString("\n")
				listHTML = b.String()
			}
			listHTML = injectLiveReload(listHTML, liveReloadURL)
			if err := s.writeOutput(filepath.Join(strings.TrimPrefix(taxonomyPathForLang(lang, s.Config.DefaultLanguage, tax.Name, ""), "/"), "index.html"), listHTML); err != nil {
				return err
			}

			for termName, entries := range termsMap {
				pathSlug := slugifyURLSegment(termName)
				ctx := s.baseTemplateContext(lang)
				ctx["current_path"] = taxonomyPathForLang(lang, s.Config.DefaultLanguage, tax.Name, pathSlug)
				ctx["current_url"] = strings.TrimRight(s.Config.BaseURL, "/") + taxonomyPathForLang(lang, s.Config.DefaultLanguage, tax.Name, pathSlug)
				termObj := map[string]any{"name": termName, "slug": pathSlug, "pages": entries, "path": taxonomyPathForLang(lang, s.Config.DefaultLanguage, tax.Name, pathSlug), "permalink": strings.TrimRight(s.Config.BaseURL, "/") + taxonomyPathForLang(lang, s.Config.DefaultLanguage, tax.Name, pathSlug)}
				ctx["taxonomy"] = map[string]any{"name": tax.Name, "term": termName, "pages": entries}
				ctx["term"] = termObj
				html, err := s.renderFirstTemplate([]string{
					tax.Name + "/single.html",
					slugifyURLSegment(tax.Name) + "/single.html",
					"taxonomy_single.html",
				}, ctx)
				if err != nil {
					html = "Category: " + termName + "\n\n\n"
					for _, entry := range entries {
						html += "    <article>\n        <h3 class=\"post__title\"><a href=\"" + fmt.Sprint(entry["permalink"]) + "\">" + fmt.Sprint(entry["title"]) + "</a></h3>\n    </article>\n"
					}
					html += "\n"
				}
				html = injectLiveReload(html, liveReloadURL)
				if err := s.writeOutput(filepath.Join(strings.TrimPrefix(taxonomyPathForLang(lang, s.Config.DefaultLanguage, tax.Name, pathSlug), "/"), "index.html"), html); err != nil {
					return err
				}
				if s.taxonomyFeedEnabled(tax.Name, lang) {
					taxPages := make([]*content.Page, 0)
					if tx, ok := s.Library.Taxonomies[tax.Name]; ok {
						if tt, ok := tx.Terms[termName]; ok {
							for _, rel := range tt.Pages {
								if p := s.Library.Pages[rel]; p != nil && p.Lang == lang {
									taxPages = append(taxPages, p)
								}
							}
						}
					}
					atom := s.defaultAtomXML(strings.TrimRight(s.Config.BaseURL, "/")+taxonomyPathForLang(lang, s.Config.DefaultLanguage, tax.Name, pathSlug)+"atom.xml", strings.TrimRight(s.Config.BaseURL, "/"), s.Config.Title+" - "+termName, lang, taxPages)
					if err := s.writeOutput(filepath.Join(strings.TrimPrefix(taxonomyPathForLang(lang, s.Config.DefaultLanguage, tax.Name, pathSlug), "/"), "atom.xml"), atom); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func taxonomyPathForLang(lang string, defaultLang string, taxName string, termSlug string) string {
	base := "/" + slugifyURLSegment(taxName)
	if lang != "" && lang != defaultLang {
		base = "/" + lang + base
	}
	if termSlug != "" {
		base += "/" + termSlug
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return base
}

func (s *Site) renderSitemap() error {
	urls := make([]string, 0, len(s.Library.Permalinks))
	for _, v := range s.Library.Permalinks {
		urls = append(urls, v)
	}
	sort.Strings(urls)
	content := s.defaultSitemapXML()
	return s.writeOutput("sitemap.xml", content)
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return s
	}
	return b.String()
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
	out := make([]*content.Page, 0, len(pages))
	for _, p := range pages {
		if p == nil {
			continue
		}
		out = append(out, p)
	}
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
		if out[i].Meta.Title != out[j].Meta.Title {
			return out[i].Meta.Title < out[j].Meta.Title
		}
		return out[i].Permalink < out[j].Permalink
	})
	return out
}

func (s *Site) defaultAtomXML(feedURL string, htmlURL string, title string, lang string, pages []*content.Page) string {
	ordered := s.sortedPagesForFeed(pages)
	updated := ""
	if len(ordered) > 0 {
		updated = formatAtomTime(ordered[0].Date)
	}
	if updated == "" {
		updated = time.Now().UTC().Format("2006-01-02T15:04:05+00:00")
	}

	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<feed xmlns=\"http://www.w3.org/2005/Atom\"")
	if lang != "" {
		b.WriteString(" xml:lang=\"")
		b.WriteString(xmlEscape(lang))
		b.WriteString("\"")
	}
	b.WriteString(">\n")
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
		b.WriteString("    <entry")
		if p.Lang != "" {
			b.WriteString(" xml:lang=\"")
			b.WriteString(xmlEscape(p.Lang))
			b.WriteString("\"")
		}
		b.WriteString(">\n")
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
		if len(authors) == 0 && strings.TrimSpace(s.Config.Author) != "" {
			authors = []string{s.Config.Author}
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
			summary := strings.ReplaceAll(*p.Summary, `<span id="continue-reading"></span>`, "")
			b.WriteString("        <summary type=\"html\">")
			b.WriteString(xmlEscape(summary))
			b.WriteString("</summary>\n")
		} else {
			b.WriteString("        <content type=\"html\" xml:base=\"")
			b.WriteString(xmlEscape(p.Permalink))
			b.WriteString("\">")
			b.WriteString(xmlEscape(p.Content))
			b.WriteString("</content>\n")
		}

		b.WriteString("    </entry>\n")
	}
	b.WriteString("</feed>")
	return b.String()
}

func (s *Site) defaultSitemapXML() string {
	lastmods := map[string]string{}
	urlsSet := map[string]struct{}{}
	for _, p := range s.Library.Pages {
		if p.Meta.Render != nil && !*p.Meta.Render {
			continue
		}
		urlsSet[p.Permalink] = struct{}{}
		if p.Date != nil {
			lastmods[p.Permalink] = p.Date.Format("2006-01-02")
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
				for i := 1; i <= n; i++ {
					urlsSet[sectionPagerPermalink(sec.Path, sec.Permalink, paginatePath, i)] = struct{}{}
				}
			}
		}
	}
	for _, tx := range s.Library.Taxonomies {
		urlsSet[strings.TrimRight(s.Config.BaseURL, "/")+taxonomyPathForLang(s.Config.DefaultLanguage, s.Config.DefaultLanguage, tx.Name, "")] = struct{}{}
		for termName := range tx.Terms {
			slug := slugifyURLSegment(termName)
			urlsSet[strings.TrimRight(s.Config.BaseURL, "/")+taxonomyPathForLang(s.Config.DefaultLanguage, s.Config.DefaultLanguage, tx.Name, slug)] = struct{}{}
		}
	}

	urls := make([]string, 0, len(urlsSet))
	for u := range urlsSet {
		urls = append(urls, u)
	}

	sort.Strings(urls)
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

func (s *Site) renderFeed() error {
	pages := make([]map[string]any, 0, len(s.Library.Pages))
	allPages := make([]*content.Page, 0, len(s.Library.Pages))
	pagesByLang := map[string][]map[string]any{}
	rawPagesByLang := map[string][]*content.Page{}
	for _, p := range s.Library.Pages {
		entry := map[string]any{"title": p.Meta.Title, "permalink": p.Permalink}
		pages = append(pages, entry)
		allPages = append(allPages, p)
		pagesByLang[p.Lang] = append(pagesByLang[p.Lang], entry)
		rawPagesByLang[p.Lang] = append(rawPagesByLang[p.Lang], p)
	}
	sort.SliceStable(pages, func(i, j int) bool {
		return pages[i]["permalink"].(string) < pages[j]["permalink"].(string)
	})
	for lang := range pagesByLang {
		sort.SliceStable(pagesByLang[lang], func(i, j int) bool {
			return pagesByLang[lang][i]["permalink"].(string) < pagesByLang[lang][j]["permalink"].(string)
		})
	}
	atom := s.defaultAtomXML(strings.TrimRight(s.Config.BaseURL, "/")+"/atom.xml", strings.TrimRight(s.Config.BaseURL, "/"), s.Config.Title, s.Config.DefaultLanguage, allPages)
	if err := s.writeOutput("atom.xml", atom); err != nil {
		return err
	}

	for lang, langPages := range pagesByLang {
		if lang == s.Config.DefaultLanguage {
			continue
		}
		opts, ok := s.Config.Languages[lang]
		if !ok || !opts.GenerateFeeds {
			continue
		}
		_ = langPages
		langAtom := s.defaultAtomXML(strings.TrimRight(s.Config.BaseURL, "/")+"/"+lang+"/atom.xml", strings.TrimRight(s.Config.BaseURL, "/")+"/"+lang+"/", s.Config.Title, lang, rawPagesByLang[lang])
		if err := s.writeOutput(filepath.Join(lang, "atom.xml"), langAtom); err != nil {
			return err
		}
	}

	for _, sec := range s.Library.Sections {
		if !sec.Meta.GenerateFeed && !sec.Meta.GenerateFeeds {
			continue
		}
		secPages := make([]*content.Page, 0, len(sec.Pages))
		for _, rel := range sec.Pages {
			if p := s.Library.Pages[rel]; p != nil {
				secPages = append(secPages, p)
			}
		}
		secAtom := s.defaultAtomXML(strings.TrimRight(s.Config.BaseURL, "/")+sec.Path+"atom.xml", strings.TrimRight(s.Config.BaseURL, "/")+sec.Path, s.Config.Title+" - "+sec.Meta.Title, sec.Lang, secPages)
		if err := s.writeOutput(filepath.Join(strings.TrimPrefix(sec.Path, "/"), "atom.xml"), secAtom); err != nil {
			return err
		}
	}
	return nil
}

func (s *Site) taxonomyFeedEnabled(name string, lang string) bool {
	if lang != "" && lang != s.Config.DefaultLanguage {
		if lo, ok := s.Config.Languages[lang]; ok {
			for _, tx := range lo.Taxonomies {
				if tx.Name == name {
					return tx.Feed
				}
			}
		}
	}
	for _, tx := range s.Config.Taxonomies {
		if tx.Name == name {
			return tx.Feed
		}
	}
	return false
}

func slugifyURLSegment(in string) string {
	in = strings.TrimSpace(strings.ToLower(in))
	if in == "" {
		return ""
	}
	var b strings.Builder
	prevDash := false
	for _, r := range in {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevDash = false
		case r == '_' || r == '-' || unicode.IsSpace(r):
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "item"
	}
	return out
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
	for _, sec := range s.Library.Sections {
		for _, alias := range sec.Meta.Aliases {
			redirect, err := s.Templates.Engine.Render("__zola_builtins/internal/alias.html", map[string]any{"url": sec.Permalink})
			if err != nil {
				redirect = "<meta http-equiv=\"refresh\" content=\"0; url=" + sec.Permalink + "\">"
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

	for _, p := range s.Library.Pages {
		if p.Meta.Render != nil && !*p.Meta.Render {
			continue
		}
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
	content = strings.ReplaceAll(content, "&#x2f;", "&#x2F;")

	lowerRel := strings.ToLower(rel)
	if strings.HasSuffix(lowerRel, ".html") || strings.HasSuffix(lowerRel, ".xml") || strings.HasSuffix(lowerRel, ".txt") || strings.HasSuffix(lowerRel, ".css") || strings.HasSuffix(lowerRel, ".js") {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
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
		pages := s.sectionPageEntries(sec)
		out[rel] = map[string]any{
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
