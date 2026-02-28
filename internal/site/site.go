package site

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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

			tplName := "page.html"
			if pg.Meta.Template != "" {
				tplName = pg.Meta.Template
			} else if sec, ok := s.Library.Sections[pg.ParentSection]; ok && sec.Meta.PageTemplate != "" {
				tplName = sec.Meta.PageTemplate
			}
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
		InsertAnchorLinks:    s.Config.Markdown.InsertAnchorLinks,
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
		if sec.Meta.Render != nil && !*sec.Meta.Render {
			continue
		}
		renderedSection, secErr := markdown.RenderContent(sec.RawContent, markdown.RenderContext{
			Permalinks:           s.Library.Permalinks,
			CurrentPagePath:      sec.RelativePath,
			CurrentPagePermalink: sec.Permalink,
			InsertAnchorLinks:    s.Config.Markdown.InsertAnchorLinks,
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
	pages := make([]map[string]any, 0, len(sec.Pages))
	for _, p := range sec.Pages {
		pg := s.Library.Pages[p]
		if pg == nil {
			continue
		}
		if pg.Meta.Render != nil && !*pg.Meta.Render {
			continue
		}
		pages = append(pages, s.pageView(p, pg))
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
					feedCtx := map[string]any{"pages": entries, "config": map[string]any{"title": s.Config.Title}, "taxonomy": map[string]any{"name": tax.Name, "term": termName}}
					atom, feedErr := s.Templates.Render("atom.xml", feedCtx)
					if feedErr != nil {
						atom = "<?xml version=\"1.0\"?><feed xmlns=\"http://www.w3.org/2005/Atom\"></feed>"
					}
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
	content, err := s.Templates.Render("sitemap.xml", map[string]any{"pages": urls})
	if err != nil {
		content = "<?xml version=\"1.0\"?><urlset></urlset>"
	}
	return s.writeOutput("sitemap.xml", content)
}

func (s *Site) renderFeed() error {
	pages := make([]map[string]any, 0, len(s.Library.Pages))
	pagesByLang := map[string][]map[string]any{}
	for _, p := range s.Library.Pages {
		entry := map[string]any{"title": p.Meta.Title, "permalink": p.Permalink}
		pages = append(pages, entry)
		pagesByLang[p.Lang] = append(pagesByLang[p.Lang], entry)
	}
	sort.SliceStable(pages, func(i, j int) bool {
		return pages[i]["permalink"].(string) < pages[j]["permalink"].(string)
	})
	for lang := range pagesByLang {
		sort.SliceStable(pagesByLang[lang], func(i, j int) bool {
			return pagesByLang[lang][i]["permalink"].(string) < pagesByLang[lang][j]["permalink"].(string)
		})
	}
	atom, err := s.Templates.Render("atom.xml", map[string]any{"pages": pages, "config": map[string]any{"title": s.Config.Title}})
	if err != nil {
		atom = "<?xml version=\"1.0\"?><feed xmlns=\"http://www.w3.org/2005/Atom\"></feed>"
	}
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
		langAtom, langErr := s.Templates.Render("atom.xml", map[string]any{"pages": langPages, "config": map[string]any{"title": s.Config.Title}, "lang": lang})
		if langErr != nil {
			langAtom = "<?xml version=\"1.0\"?><feed xmlns=\"http://www.w3.org/2005/Atom\"></feed>"
		}
		if err := s.writeOutput(filepath.Join(lang, "atom.xml"), langAtom); err != nil {
			return err
		}
	}

	for _, sec := range s.Library.Sections {
		if !sec.Meta.GenerateFeed && !sec.Meta.GenerateFeeds {
			continue
		}
		entries := s.sectionPageEntries(sec)
		secAtom, secErr := s.Templates.Render("atom.xml", map[string]any{"pages": entries, "config": map[string]any{"title": s.Config.Title}, "section": map[string]any{"path": sec.Path, "title": sec.Meta.Title}})
		if secErr != nil {
			secAtom = "<?xml version=\"1.0\"?><feed xmlns=\"http://www.w3.org/2005/Atom\"></feed>"
		}
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
