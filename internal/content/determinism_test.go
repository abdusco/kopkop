package content

import (
	"testing"

	"github.com/abdusco/kopkop/internal/config"
	"github.com/stretchr/testify/require"
)

func TestNestedTransparentSections(t *testing.T) {
	for _, tc := range []struct {
		name        string
		transparent bool
		rootPages   []string
	}{
		{"propagate through ancestors", true, []string{"a/b/post.md", "a/post.md", "root.md"}},
		{"stop at opaque ancestor", false, []string{"root.md"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for range 100 {
				lib := &Library{
					Pages: map[string]*Page{
						"root.md":     {ParentSection: "_index.md"},
						"a/post.md":   {ParentSection: "a/_index.md"},
						"a/b/post.md": {ParentSection: "a/b/_index.md", Meta: PageFrontMatter{Taxonomies: map[string][]string{"tags": {"go", "go"}}}},
					},
					Sections: map[string]*Section{
						"_index.md":     {RelativePath: "_index.md"},
						"a/_index.md":   {RelativePath: "a/_index.md", Meta: SectionFrontMatter{Transparent: tc.transparent}},
						"a/b/_index.md": {RelativePath: "a/b/_index.md", Meta: SectionFrontMatter{Transparent: true}},
					},
					Taxonomies: map[string]*Taxonomy{},
				}
				lib.Pages["root.md"].Meta.Taxonomies = map[string][]string{"tags": {"go"}}
				attachPagesToSections(lib)
				require.Equal(t, tc.rootPages, lib.Sections["_index.md"].Pages)
				require.Equal(t, []string{"a/b/post.md", "a/post.md"}, lib.Sections["a/_index.md"].Pages)
				buildTaxonomies(lib, config.Default())
				require.Equal(t, []string{"a/b/post.md", "root.md"}, lib.Taxonomies["tags"].Terms["go"].Pages)
			}
		})
	}
}
