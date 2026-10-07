package site

import (
	"cmp"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/abdusco/kopkop/internal/filesystem"
	"github.com/abdusco/kopkop/internal/slug"
)

type outputClaim struct {
	path      string
	source    string
	directory bool
}

// Validate all statically known output ownership before templates (including
// shortcode templates) run or an existing output directory is removed.
func (s *Site) validateOutputManifest() error {
	var claims []outputClaim
	add := func(name, source string) {
		claims = append(claims, outputClaim{path: filepath.ToSlash(name), source: source})
	}
	for rel, pg := range s.Library.Pages {
		if pg.Meta.Render == nil || *pg.Meta.Render {
			add(filepath.Join(strings.TrimPrefix(pg.Path, "/"), "index.html"), "page "+rel)
			for _, alias := range pg.Meta.Aliases {
				add(aliasOutputPath(alias), "alias "+alias+" of page "+rel)
			}
		}
		// Unpublished bundles still copy assets.
		for _, asset := range pg.Assets {
			source, err := filepath.Rel(s.BasePath, asset)
			if err != nil {
				return err
			}
			add(filepath.Join(strings.TrimPrefix(pg.Path, "/"), filepath.Base(asset)), "asset "+filepath.ToSlash(source))
		}
	}
	for rel, sec := range s.Library.Sections {
		if sec.Meta.Render == nil || *sec.Meta.Render {
			for _, plan := range s.sectionRenderPlans(sec, s.sectionPageEntries(sec)) {
				if strings.TrimSpace(sec.Meta.RedirectTo) != "" && plan.OutputPath != filepath.Join(strings.TrimPrefix(sec.Path, "/"), "index.html") {
					continue
				}
				add(plan.OutputPath, "section "+rel)
			}
			if sec.Meta.PaginateBy > 0 {
				paginatePath := cmp.Or(strings.Trim(sec.Meta.PaginatePath, "/"), "page")
				add(filepath.Join(strings.TrimPrefix(sec.Path, "/"), paginatePath, "1", "index.html"), "pagination alias of section "+rel)
			}
		}
		// Match renderAliases: section aliases are published even for render=false.
		for _, alias := range sec.Meta.Aliases {
			add(aliasOutputPath(alias), "alias "+alias+" of section "+rel)
		}
		if s.Config.GenerateFeeds && (sec.Meta.GenerateFeed || sec.Meta.GenerateFeeds) {
			for _, name := range s.feedFilenames() {
				add(filepath.Join(strings.TrimPrefix(sec.Path, "/"), name), "feed "+name+" of section "+rel)
			}
		}
	}
	if _, root := s.Library.Sections["_index.md"]; !root && s.templateExists("index.html") {
		add("index.html", "implicit homepage")
	}
	for _, tax := range s.Library.Taxonomies {
		base := cmp.Or(slug.Normalize(tax.Name), "item")
		add(path.Join(base, "index.html"), "taxonomy "+tax.Name)
		for term := range tax.Terms {
			termPath := path.Join(base, cmp.Or(slug.Normalize(term), "item"))
			add(path.Join(termPath, "index.html"), "term "+term+" of taxonomy "+tax.Name)
			if s.taxonomyFeedEnabled(tax.Name) {
				for _, name := range s.feedFilenames() {
					add(path.Join(termPath, name), "feed "+name+" of term "+term+" of taxonomy "+tax.Name)
				}
			}
		}
	}
	// A static file with the same name replaces the generated artifact.
	addGenerated := func(name, source string) {
		if !s.hasStaticFile(name) {
			add(name, source)
		}
	}
	addGenerated("404.html", "generated 404 page")
	if s.Config.GenerateSitemap {
		addGenerated("sitemap.xml", "generated sitemap")
	}
	if s.Config.GenerateRobotsTXT {
		addGenerated("robots.txt", "generated robots.txt")
	}
	if s.Config.GenerateFeeds {
		for _, name := range s.feedFilenames() {
			addGenerated(name, "site feed "+name)
		}
	}
	if s.Config.BuildSearchIndex || s.Config.Search.BuildIndex {
		addGenerated(s.Config.Search.IndexPath, "search index")
	}
	// Reserve this name whenever highlighting is enabled, even if a template
	// ultimately omits highlighted content or a <head> element.
	if s.highlightCSSPath != "" {
		add(s.highlightCSSPath, "highlight stylesheet")
	}

	// A matching site static file explicitly overrides its theme counterpart.
	// Directory/file conflicts and collisions with generated files still fail.
	static := map[string]outputClaim{}
	roots := []string{"static"}
	if s.Config.Theme != "" {
		roots = append([]string{path.Join("themes", s.Config.Theme, "static")}, roots...)
	}
	for _, root := range roots {
		err := fs.WalkDir(s.Templates.SourceFS, root, func(name string, d fs.DirEntry, err error) error {
			if os.IsNotExist(err) && name == root {
				return nil
			}
			if err != nil {
				return err
			}
			if name == root {
				if !d.IsDir() {
					return fmt.Errorf("static root %q must be a directory: %w", root, fs.ErrInvalid)
				}
				return nil
			}
			rel := strings.TrimPrefix(name, root+"/")
			claim := outputClaim{path: rel, source: "static " + name, directory: d.IsDir()}
			if old, ok := static[rel]; ok && old.directory != claim.directory {
				return fmt.Errorf("output collision at %q between %s and %s (file/directory)", rel, old.source, claim.source)
			}
			static[rel] = claim
			return nil
		})
		if err != nil {
			return fmt.Errorf("inspect static outputs: %w", err)
		}
	}
	for _, claim := range static {
		claims = append(claims, claim)
	}
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].path != claims[j].path {
			return claims[i].path < claims[j].path
		}
		return claims[i].source < claims[j].source
	})
	files := map[string]outputClaim{}
	directories := map[string]outputClaim{}
	for _, claim := range claims {
		if err := filesystem.ValidatePath(claim.path); err != nil {
			return fmt.Errorf("invalid output path of %s: %w", claim.source, err)
		}
		if claim.path == "." {
			return fmt.Errorf("invalid file output of %s: %w", claim.source, fs.ErrInvalid)
		}
		// Use a portable namespace, including hosts with case-insensitive disks.
		key := strings.ToLower(claim.path)
		if old, ok := files[key]; ok {
			// Multiple language variants can share the very same bundle asset.
			if !claim.directory && old.source == claim.source && strings.HasPrefix(claim.source, "asset ") {
				continue
			}
			return fmt.Errorf("output collision at %q between %s and %s", claim.path, old.source, claim.source)
		}
		if !claim.directory {
			if old, ok := directories[key]; ok {
				return fmt.Errorf("output collision at %q between %s and %s (file/directory)", claim.path, old.source, claim.source)
			}
			files[key] = claim
		} else {
			directories[key] = claim
		}
		for parent := path.Dir(key); parent != "."; parent = path.Dir(parent) {
			if old, ok := files[parent]; ok {
				return fmt.Errorf("output collision at %q between %s and %s (file/directory)", parent, old.source, claim.source)
			}
			directories[parent] = claim
		}
	}
	return nil
}

func aliasOutputPath(alias string) string {
	name := strings.TrimPrefix(alias, "/")
	if strings.HasSuffix(name, ".html") {
		return filepath.FromSlash(name)
	}
	return filepath.Join(filepath.FromSlash(name), "index.html")
}

// hasStaticFile reports whether the site or its theme ships rel under static/.
func (s *Site) hasStaticFile(rel string) bool {
	roots := []string{"static"}
	if s.Config.Theme != "" {
		roots = append(roots, path.Join("themes", s.Config.Theme, "static"))
	}
	for _, root := range roots {
		if info, err := s.Templates.SourceFS.Stat(path.Join(root, filepath.ToSlash(rel))); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}
