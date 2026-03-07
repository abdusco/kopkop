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
	"github.com/samber/lo"
)

type LoadOptions struct {
	IncludeDrafts  bool
	RenderMarkdown bool
}

type contentFile struct {
	AbsPath   string
	RelPath   string
	IsSection bool
}

type loadedContent struct {
	File    contentFile
	Page    *Page
	Section *Section
}

func LoadLibrary(basePath string, cfg config.Config, opts LoadOptions) (*Library, error) {
	lib := NewLibrary()
	contentDir := filepath.Join(basePath, "content")
	draftsEnabled := opts.IncludeDrafts || cfg.EnableDraftsInBuild

	files, err := collectContentFiles(contentDir)
	if err != nil {
		return nil, err
	}

	files = lo.Filter(files, func(file contentFile, _ int) bool {
		return !shouldIgnoreContent(file.RelPath, cfg.IgnoredContent)
	})

	loaded := make([]loadedContent, 0, len(files))
	for _, file := range files {
		raw, readErr := os.ReadFile(file.AbsPath)
		if readErr != nil {
			return nil, readErr
		}

		if file.IsSection {
			sec, loadErr := parseSection(file.AbsPath, file.RelPath, string(raw), cfg)
			if loadErr != nil {
				return nil, loadErr
			}
			loaded = append(loaded, loadedContent{File: file, Section: sec})
			continue
		}

		page, loadErr := parsePage(file.AbsPath, file.RelPath, string(raw), cfg)
		if loadErr != nil {
			return nil, loadErr
		}
		if page.Meta.Draft && !draftsEnabled {
			continue
		}

		loaded = append(loaded, loadedContent{File: file, Page: page})
	}

	if !draftsEnabled {
		loaded = filterDraftContent(loaded)
	}

	for _, item := range loaded {
		if item.Section == nil {
			continue
		}
		lib.Sections[item.File.RelPath] = item.Section
		lib.Permalinks[item.File.RelPath] = item.Section.Permalink
	}
	for _, item := range loaded {
		if item.Page == nil {
			continue
		}
		lib.Pages[item.File.RelPath] = item.Page
		lib.Permalinks[item.File.RelPath] = item.Page.Permalink
	}

	if opts.RenderMarkdown {
		for _, item := range loaded {
			if item.Page == nil {
				continue
			}
			page := item.Page
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
	}

	attachPagesToSections(lib)
	attachSubsections(lib)
	buildTaxonomies(lib, cfg)
	return lib, nil
}

func filterDraftContent(items []loadedContent) []loadedContent {
	hiddenPrefixes := lo.Uniq(lo.Map(
		lo.Filter(items, func(item loadedContent, _ int) bool {
			return item.Section != nil && item.Section.Meta.Draft
		}),
		func(item loadedContent, _ int) string {
			dir := filepath.ToSlash(filepath.Dir(item.File.RelPath))
			if dir == "." {
				dir = ""
			}
			return dir
		},
	))

	return lo.Filter(items, func(item loadedContent, _ int) bool {
		if item.Page != nil && item.Page.Meta.Draft {
			return false
		}
		if item.Section != nil && item.Section.Meta.Draft {
			return false
		}
		if len(hiddenPrefixes) == 0 {
			return true
		}

		dir := filepath.ToSlash(filepath.Dir(item.File.RelPath))
		if dir == "." {
			dir = ""
		}

		return !lo.SomeBy(hiddenPrefixes, func(prefix string) bool {
			return prefix == "" || dir == prefix || strings.HasPrefix(dir, prefix+"/")
		})
	})
}

func collectContentFiles(contentDir string) ([]contentFile, error) {
	files := []contentFile{}
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

		rel, relErr := filepath.Rel(contentDir, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		files = append(files, contentFile{
			AbsPath:   path,
			RelPath:   rel,
			IsSection: strings.HasPrefix(filepath.Base(path), "_index"),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.SliceStable(files, func(i, j int) bool {
		return files[i].RelPath < files[j].RelPath
	})

	return files, nil
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

	base := filepath.Base(relPath)
	ext := filepath.Ext(base)
	baseName := strings.TrimSuffix(base, ext)
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
	p := pathing.ComputePagePath(meta.Path, slug, components, strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath)), hasColocated)
	permalink := pathing.MakePermalink(cfg.BaseURL, p)

	page := &Page{
		SourcePath:    absPath,
		RelativePath:  relPath,
		Meta:          meta,
		RawContent:    body,
		Slug:          slug,
		Path:          p,
		Permalink:     permalink,
		Components:    splitComponents(strings.Trim(p, "/")),
		ParentSection: parentSectionPath(relPath),
	}

	if t, ok := parseDateAny(meta.Date); ok {
		page.Date = &t
	} else if extractedDate != "" {
		if t, ok := parseDateAny(extractedDate); ok {
			page.Date = &t
		}
	}
	if t, ok := parseDateAny(meta.Updated); ok {
		page.Updated = &t
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

	dir := filepath.Dir(relPath)
	if dir == "." {
		dir = ""
	}
	p := "/"
	if dir != "" {
		p = "/" + filepath.ToSlash(dir) + "/"
	}
	permalink := pathing.MakePermalink(cfg.BaseURL, p)

	return &Section{
		SourcePath:   absPath,
		RelativePath: relPath,
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
			next := parentSectionFromSectionPath(parent)
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
		parent := parentSectionFromSectionPath(sec.RelativePath)
		if target, ok := lib.Sections[parent]; ok {
			target.Pages = append(target.Pages, sec.Pages...)
		}
	}

	for _, sec := range lib.Sections {
		sec.Pages = lo.Uniq(sec.Pages)

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

func parentSectionFromSectionPath(sectionRelPath string) string {
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
	return fmt.Sprintf("%s/_index.md", parentDir)
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
		sort.SliceStable(lib.Sections[parentKey].Subsections, func(i, j int) bool {
			left := lib.Sections[parentKey].Subsections[i]
			right := lib.Sections[parentKey].Subsections[j]
			lw := lib.Sections[left].Meta.Weight
			rw := lib.Sections[right].Meta.Weight
			if lw != rw {
				return lw < rw
			}
			return left < right
		})
	}
}

func countSegments(p string) int {
	p = strings.Trim(p, "/")
	if p == "" {
		return 0
	}
	return len(strings.Split(p, "/"))
}

func parseDateAny(v any) (time.Time, bool) {
	switch x := v.(type) {
	case nil:
		return time.Time{}, false
	case time.Time:
		if x.Hour() == 0 && x.Minute() == 0 && x.Second() == 0 && x.Nanosecond() == 0 {
			return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, time.UTC), true
		}
		return x.UTC(), true
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

func parentSectionPath(rel string) string {
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." || dir == "" {
		return "_index.md"
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
