package site

import (
	"github.com/abdusco/kopkop/internal/content"
	"github.com/mitsuhiko/minijinja/minijinja-go/v2/value"
)

// Views are built before page workers start and stay read-only until the next
// build. Store MiniJinja values as well as Go views so rendering does not
// recursively convert the full library for every output template.
type templateViews struct {
	rawPages     map[string]any
	pages        map[string]value.Value
	sections     map[string]value.Value
	sectionPages map[*content.Section][]map[string]any
	base         map[string]any
}

func (s *Site) prepareTemplateViews() {
	s.templateViews = nil
	views := &templateViews{
		rawPages:     s.serializedPages(),
		pages:        make(map[string]value.Value, len(s.Library.Pages)),
		sections:     make(map[string]value.Value, len(s.Library.Sections)),
		sectionPages: make(map[*content.Section][]map[string]any, len(s.Library.Sections)),
	}
	s.templateViews = views
	for rel, page := range views.rawPages {
		views.pages[rel] = value.FromAny(page)
	}
	for rel, sec := range s.Library.Sections {
		entries := s.sectionPageEntries(sec)
		views.sectionPages[sec] = entries
		pages := make([]value.Value, len(entries))
		for i, page := range entries {
			pages[i] = views.pages[page["relative_path"].(string)]
		}
		section := s.sectionView(rel, sec, entries)
		section["pages"] = value.FromSlice(pages)
		views.sections[rel] = value.FromAny(section)
	}
	views.base = map[string]any{
		"config":       value.FromAny(s.Config.TemplateView()),
		"__pages":      value.FromMap(views.pages),
		"__sections":   value.FromMap(views.sections),
		"__taxonomies": value.FromAny(s.serializedTaxonomies()),
	}
}
