package content

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/abdusco/kopkop/internal/config"
	"github.com/abdusco/kopkop/internal/content/frontmatter"
	"github.com/abdusco/kopkop/internal/content/pathing"
	"github.com/abdusco/kopkop/internal/markdown"
)

type LoadOptions struct {
	IncludeDrafts  bool
	RenderMarkdown bool
}

func LoadLibrary(basePath string, cfg config.Config, opts LoadOptions) (*Library, error) {
	lib := NewLibrary()
	contentDir := filepath.Join(basePath, "content")

	err := filepath.WalkDir(contentDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}

		rel, err := filepath.Rel(contentDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if shouldIgnoreContent(rel, cfg.IgnoredContent) {
			return nil
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		name := filepath.Base(path)
		if strings.HasPrefix(name, "_index") {
			sec, loadErr := parseSection(path, rel, string(raw), cfg)
			if loadErr != nil {
				return loadErr
			}
			lib.Sections[rel] = sec
			lib.Permalinks[rel] = sec.Permalink
			return nil
		}

		page, loadErr := parsePage(path, rel, string(raw), cfg)
		if loadErr != nil {
			return loadErr
		}
		if page.Meta.Draft && !opts.IncludeDrafts && !cfg.EnableDraftsInBuild {
			return nil
		}
		if opts.RenderMarkdown {
			res, renderErr := markdown.RenderContent(page.RawContent, markdown.RenderContext{
				Permalinks:           lib.Permalinks,
				CurrentPagePath:      page.RelativePath,
				CurrentPagePermalink: page.Permalink,
				InsertAnchorLinks:    cfg.Markdown.InsertAnchorLinks,
			})
			if renderErr == nil {
				page.Content = res.Body
				page.Summary = res.Summary
				for _, h := range res.TOC {
					page.TOC = append(page.TOC, Heading{ID: h.ID, Level: h.Level, Title: h.Title})
				}
				for _, il := range res.InternalLinks {
					page.InternalLinks = append(page.InternalLinks, InternalLink{Path: il.Path, Anchor: il.Anchor})
				}
				page.ExternalLinks = append(page.ExternalLinks, res.ExternalLinks...)
			}
		}

		lib.Pages[rel] = page
		lib.Permalinks[rel] = page.Permalink
		return nil
	})
	if err != nil {
		return nil, err
	}

	if !opts.IncludeDrafts && !cfg.EnableDraftsInBuild {
		filterDraftSections(lib)
	}

	attachPagesToSections(lib)
	attachSubsections(lib)
	attachTranslations(lib, cfg.DefaultLanguage)
	buildTaxonomies(lib, cfg)
	return lib, nil
}

func shouldIgnoreContent(rel string, patterns []string) bool {
	base := filepath.Base(rel)
	if strings.HasPrefix(base, ".") {
		return true
	}
	for _, p := range patterns {
		p = filepath.ToSlash(p)
		if match, _ := filepath.Match(p, rel); match {
			return true
		}
		if strings.HasPrefix(p, "**/") {
			trim := strings.TrimPrefix(p, "**/")
			if match, _ := filepath.Match(trim, base); match {
				return true
			}
		}
	}
	return false
}

func parsePage(absPath, relPath, content string, cfg config.Config) (*Page, error) {
	meta, body, err := parsePageFrontMatterOptional(relPath, content)
	if err != nil {
		return nil, err
	}

	lang := inferLangFromFilename(relPath, cfg.DefaultLanguage)
	baseName := baseNameForSlug(relPath, lang, cfg.DefaultLanguage)
	filePathForSlug := baseName
	if baseName == "index" {
		filePathForSlug = filepath.Base(filepath.Dir(relPath))
	}
	components := splitComponents(filepath.Dir(relPath))
	slug, extractedDate := pathing.ComputePageSlug(meta.Slug, filePathForSlug, cfg.PathsKeepDates)
	hasColocated, _ := hasColocatedAssets(absPath)
	if baseName == "index" {
		slug = ""
	}
	p := pathing.ComputePagePath(meta.Path, slug, components, strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath)), hasColocated, lang, cfg.DefaultLanguage)
	permalink := pathing.MakePermalink(cfg.BaseURL, p)

	page := &Page{
		SourcePath:    absPath,
		RelativePath:  relPath,
		Lang:          lang,
		Meta:          meta,
		RawContent:    body,
		Slug:          slug,
		Path:          p,
		Permalink:     permalink,
		Components:    splitComponents(strings.Trim(p, "/")),
		ParentSection: parentSectionPath(relPath, lang, cfg.DefaultLanguage),
	}

	if t, ok := parseDateAny(meta.Date); ok {
		page.Date = &t
	} else if extractedDate != "" {
		if t, e := time.Parse(time.RFC3339, extractedDate); e == nil {
			page.Date = &t
		}
	}

	assets, _ := findColocatedAssets(absPath)
	page.Assets = assets

	return page, nil
}

func hasColocatedAssets(pageAbsPath string) (bool, error) {
	assets, err := findColocatedAssets(pageAbsPath)
	if err != nil {
		return false, err
	}
	return len(assets) > 0, nil
}

func parseSection(absPath, relPath, content string, cfg config.Config) (*Section, error) {
	meta, body, err := parseSectionFrontMatterOptional(relPath, content)
	if err != nil {
		return nil, err
	}

	lang := inferLangFromFilename(relPath, cfg.DefaultLanguage)
	dir := filepath.Dir(relPath)
	if dir == "." {
		dir = ""
	}
	p := "/"
	if dir != "" {
		p = "/" + filepath.ToSlash(dir) + "/"
	}
	if lang != cfg.DefaultLanguage {
		p = "/" + lang + "/" + strings.TrimPrefix(p, "/")
		p = strings.ReplaceAll(p, "//", "/")
		if !strings.HasSuffix(p, "/") {
			p += "/"
		}
	}
	permalink := pathing.MakePermalink(cfg.BaseURL, p)

	return &Section{
		SourcePath:   absPath,
		RelativePath: relPath,
		Lang:         lang,
		Meta:         meta,
		RawContent:   body,
		Path:         p,
		Permalink:    permalink,
		Components:   splitComponents(strings.Trim(p, "/")),
		Pages:        []string{},
		Subsections:  []string{},
	}, nil
}

func attachPagesToSections(lib *Library) {
	for rel, p := range lib.Pages {
		if sec, ok := lib.Sections[p.ParentSection]; ok {
			sec.Pages = append(sec.Pages, rel)
			continue
		}
		base := filepath.Base(rel)
		if base != "index.md" && !strings.HasPrefix(base, "index.") {
			continue
		}
		for parent := p.ParentSection; ; {
			next := parentSectionFromSectionPath(parent, sectionLangSuffix(parent))
			if next == parent {
				break
			}
			if sec, ok := lib.Sections[next]; ok {
				sec.Pages = append(sec.Pages, rel)
				break
			}
			parent = next
		}
	}

	for _, sec := range lib.Sections {
		if !sec.Meta.Transparent {
			continue
		}
		parent := parentSectionFromSectionPath(sec.RelativePath, sectionLangSuffix(sec.RelativePath))
		if target, ok := lib.Sections[parent]; ok {
			target.Pages = append(target.Pages, sec.Pages...)
		}
	}

	for _, sec := range lib.Sections {
		dedup := map[string]struct{}{}
		uniq := make([]string, 0, len(sec.Pages))
		for _, p := range sec.Pages {
			if _, ok := dedup[p]; ok {
				continue
			}
			dedup[p] = struct{}{}
			uniq = append(uniq, p)
		}
		sec.Pages = uniq

		sortBy := strings.ToLower(strings.TrimSpace(sec.Meta.SortBy))
		if sortBy == "date" {
			sort.SliceStable(sec.Pages, func(i, j int) bool {
				pi := lib.Pages[sec.Pages[i]]
				pj := lib.Pages[sec.Pages[j]]
				if pi.Date != nil && pj.Date != nil {
					if !pi.Date.Equal(*pj.Date) {
						return pi.Date.After(*pj.Date)
					}
				}
				if pi.Date != nil && pj.Date == nil {
					return true
				}
				if pi.Date == nil && pj.Date != nil {
					return false
				}
				return sec.Pages[i] < sec.Pages[j]
			})
			continue
		}
		if sortBy == "weight" {
			sort.SliceStable(sec.Pages, func(i, j int) bool {
				pi := lib.Pages[sec.Pages[i]]
				pj := lib.Pages[sec.Pages[j]]
				if pi.Meta.Weight != pj.Meta.Weight {
					return pi.Meta.Weight < pj.Meta.Weight
				}
				return sec.Pages[i] < sec.Pages[j]
			})
			continue
		}
		sort.SliceStable(sec.Pages, func(i, j int) bool {
			return sec.Pages[i] < sec.Pages[j]
		})
	}
}

func buildTaxonomies(lib *Library, cfg config.Config) {
	for _, tax := range cfg.Taxonomies {
		lib.Taxonomies[tax.Name] = &Taxonomy{Name: tax.Name, Terms: map[string]*TaxonomyTerm{}}
	}
	for rel, p := range lib.Pages {
		for taxName, values := range p.Meta.Taxonomies {
			tax, ok := lib.Taxonomies[taxName]
			if !ok {
				tax = &Taxonomy{Name: taxName, Terms: map[string]*TaxonomyTerm{}}
				lib.Taxonomies[taxName] = tax
			}
			for _, v := range values {
				term := tax.Terms[v]
				if term == nil {
					term = &TaxonomyTerm{Name: v, Pages: []string{}}
					tax.Terms[v] = term
				}
				term.Pages = append(term.Pages, rel)
			}
		}
	}
}

func filterDraftSections(lib *Library) {
	hiddenPrefixes := []string{}
	for rel, sec := range lib.Sections {
		if sec.Meta.Draft {
			dir := filepath.ToSlash(filepath.Dir(rel))
			if dir == "." {
				dir = ""
			}
			hiddenPrefixes = append(hiddenPrefixes, dir)
			delete(lib.Sections, rel)
		}
	}
	for rel := range lib.Sections {
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." {
			dir = ""
		}
		for _, prefix := range hiddenPrefixes {
			if prefix == "" || dir == prefix || strings.HasPrefix(dir, prefix+"/") {
				delete(lib.Sections, rel)
				break
			}
		}
	}
	if len(hiddenPrefixes) == 0 {
		return
	}
	for rel := range lib.Pages {
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." {
			dir = ""
		}
		for _, prefix := range hiddenPrefixes {
			if prefix == "" || dir == prefix || strings.HasPrefix(dir, prefix+"/") {
				delete(lib.Pages, rel)
				break
			}
		}
	}
}

func parentSectionFromSectionPath(sectionRelPath string, lang string) string {
	dir := filepath.ToSlash(filepath.Dir(sectionRelPath))
	if dir == "." || dir == "" {
		if lang != "" {
			return "_index." + lang + ".md"
		}
		return "_index.md"
	}
	parentDir := filepath.ToSlash(filepath.Dir(dir))
	if parentDir == "." {
		parentDir = ""
	}
	if parentDir == "" {
		if lang != "" {
			return "_index." + lang + ".md"
		}
		return "_index.md"
	}
	if lang != "" {
		return fmt.Sprintf("%s/_index.%s.md", parentDir, lang)
	}
	return fmt.Sprintf("%s/_index.md", parentDir)
}

func sectionLangSuffix(sectionRelPath string) string {
	base := filepath.Base(sectionRelPath)
	parts := strings.Split(base, ".")
	if len(parts) >= 3 {
		return parts[len(parts)-2]
	}
	return ""
}

func attachSubsections(lib *Library) {
	sectionKeys := make([]string, 0, len(lib.Sections))
	for k := range lib.Sections {
		sectionKeys = append(sectionKeys, k)
	}
	sort.Strings(sectionKeys)

	for _, parentKey := range sectionKeys {
		parentDir := filepath.ToSlash(filepath.Dir(parentKey))
		for _, childKey := range sectionKeys {
			if childKey == parentKey {
				continue
			}
			childDir := filepath.ToSlash(filepath.Dir(childKey))
			if childDir == "." {
				childDir = ""
			}
			if parentDir == "." {
				parentDir = ""
			}
			if parentDir == "" {
				if countSegments(childDir) == 1 {
					lib.Sections[parentKey].Subsections = append(lib.Sections[parentKey].Subsections, childKey)
				}
				continue
			}
			prefix := parentDir + "/"
			if strings.HasPrefix(childDir, prefix) {
				rest := strings.TrimPrefix(childDir, prefix)
				if countSegments(rest) == 1 {
					lib.Sections[parentKey].Subsections = append(lib.Sections[parentKey].Subsections, childKey)
				}
			}
		}
		sort.Strings(lib.Sections[parentKey].Subsections)
	}
}

func attachTranslations(lib *Library, defaultLang string) {
	groups := map[string][]string{}
	for rel, p := range lib.Pages {
		groups[translationKey(rel, p.Lang, defaultLang)] = append(groups[translationKey(rel, p.Lang, defaultLang)], rel)
	}
	for _, paths := range groups {
		sort.Strings(paths)
		for _, rel := range paths {
			others := make([]string, 0, len(paths)-1)
			for _, p := range paths {
				if p == rel {
					continue
				}
				others = append(others, p)
			}
			lib.Pages[rel].Translations = others
		}
	}
}

func translationKey(rel string, lang string, defaultLang string) string {
	base := filepath.Base(rel)
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." {
		dir = ""
	}
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if lang != "" && lang != defaultLang {
		suffix := "." + lang
		if strings.HasSuffix(name, suffix) {
			name = strings.TrimSuffix(name, suffix)
		}
	}
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

func countSegments(p string) int {
	p = strings.Trim(p, "/")
	if p == "" {
		return 0
	}
	return len(strings.Split(p, "/"))
}

func inferLangFromFilename(relPath string, def string) string {
	base := filepath.Base(relPath)
	parts := strings.Split(base, ".")
	if len(parts) >= 3 {
		return parts[len(parts)-2]
	}
	return def
}

func baseNameForSlug(relPath string, lang string, defaultLang string) string {
	base := filepath.Base(relPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if lang != "" && lang != defaultLang {
		suffix := "." + lang
		if strings.HasSuffix(name, suffix) {
			name = strings.TrimSuffix(name, suffix)
		}
	}
	return name
}

func parseDateAny(v any) (time.Time, bool) {
	switch x := v.(type) {
	case nil:
		return time.Time{}, false
	case time.Time:
		return x, true
	case string:
		if x == "" {
			return time.Time{}, false
		}
		if t, err := time.Parse(time.RFC3339, x); err == nil {
			return t, true
		}
		if t, err := time.Parse("2006-01-02", x); err == nil {
			return t, true
		}
		return time.Time{}, false
	default:
		return time.Time{}, false
	}
}

func splitComponents(p string) []string {
	p = filepath.ToSlash(strings.Trim(p, "/"))
	if p == "" || p == "." {
		return []string{}
	}
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		out = append(out, part)
	}
	return out
}

func parentSectionPath(rel string, lang string, defaultLang string) string {
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." || dir == "" {
		if lang != "" && lang != defaultLang {
			return "_index." + lang + ".md"
		}
		return "_index.md"
	}
	if lang != "" && lang != defaultLang {
		return fmt.Sprintf("%s/_index.%s.md", dir, lang)
	}
	return fmt.Sprintf("%s/_index.md", dir)
}

func findColocatedAssets(pageAbsPath string) ([]string, error) {
	name := strings.TrimSuffix(filepath.Base(pageAbsPath), filepath.Ext(pageAbsPath))
	if name != "index" && !strings.HasPrefix(name, "index.") {
		return []string{}, nil
	}
	dir := filepath.Dir(pageAbsPath)
	ent, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, e := range ent {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".md") {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	sort.Strings(out)
	return out, nil
}

func parsePageFrontMatterOptional(relPath string, contentStr string) (PageFrontMatter, string, error) {
	contentStr = strings.TrimPrefix(contentStr, "\ufeff")
	meta, body, err := frontmatter.ParseFrontMatter[PageFrontMatter](relPath, contentStr)
	if err == nil {
		return meta, body, nil
	}
	trim := strings.TrimLeft(contentStr, " \t\r\n")
	if strings.HasPrefix(trim, "+++") || strings.HasPrefix(trim, "---") {
		return PageFrontMatter{}, "", err
	}
	return PageFrontMatter{}, contentStr, nil
}

func parseSectionFrontMatterOptional(relPath string, contentStr string) (SectionFrontMatter, string, error) {
	contentStr = strings.TrimPrefix(contentStr, "\ufeff")
	meta, body, err := frontmatter.ParseFrontMatter[SectionFrontMatter](relPath, contentStr)
	if err == nil {
		return meta, body, nil
	}
	trim := strings.TrimLeft(contentStr, " \t\r\n")
	if strings.HasPrefix(trim, "+++") || strings.HasPrefix(trim, "---") {
		return SectionFrontMatter{}, "", err
	}
	return SectionFrontMatter{}, contentStr, nil
}
