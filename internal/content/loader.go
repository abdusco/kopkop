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

	attachPagesToSections(lib)
	buildTaxonomies(lib, cfg)
	return lib, nil
}

func parsePage(absPath, relPath, content string, cfg config.Config) (*Page, error) {
	meta, body, err := frontmatter.ParseFrontMatter[PageFrontMatter](relPath, content)
	if err != nil {
		return nil, err
	}

	lang := inferLangFromFilename(relPath, cfg.DefaultLanguage)
	fileName := baseNameForSlug(relPath, lang, cfg.DefaultLanguage)
	if fileName == "index" {
		fileName = filepath.Base(filepath.Dir(relPath))
	}
	components := splitComponents(filepath.Dir(relPath))
	slug, extractedDate := pathing.ComputePageSlug(meta.Slug, fileName, cfg.PathsKeepDates)
	p := pathing.ComputePagePath(meta.Path, slug, components, strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath)), false, lang, cfg.DefaultLanguage)
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

	if meta.Date != "" {
		if t, e := time.Parse(time.RFC3339, meta.Date); e == nil {
			page.Date = &t
		}
	} else if extractedDate != "" {
		if t, e := time.Parse(time.RFC3339, extractedDate); e == nil {
			page.Date = &t
		}
	}

	assets, _ := findColocatedAssets(absPath)
	page.Assets = assets

	return page, nil
}

func parseSection(absPath, relPath, content string, cfg config.Config) (*Section, error) {
	meta, body, err := frontmatter.ParseFrontMatter[SectionFrontMatter](relPath, content)
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
		p = "/" + lang + strings.TrimPrefix(p, "/")
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
	}, nil
}

func attachPagesToSections(lib *Library) {
	for rel, p := range lib.Pages {
		if sec, ok := lib.Sections[p.ParentSection]; ok {
			sec.Pages = append(sec.Pages, rel)
		}
	}
	for _, sec := range lib.Sections {
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
				continue
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
	if strings.TrimSuffix(filepath.Base(pageAbsPath), filepath.Ext(pageAbsPath)) != "index" {
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
