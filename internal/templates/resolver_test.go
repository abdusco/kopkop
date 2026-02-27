package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolverResolve_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		template  string
		theme     string
		available map[string]struct{}
		want      string
		wantErr   bool
	}{
		{
			name:     "site template has highest precedence",
			template: "page.html",
			theme:    "hyde",
			available: map[string]struct{}{
				"page.html":                           {},
				"hyde/templates/page.html":            {},
				"__zola_builtins/page.html":           {},
				"__zola_builtins/section.html":        {},
				"hyde/templates/section.html":         {},
				"__zola_builtins/internal/alias.html": {},
			},
			want: "page.html",
		},
		{
			name:     "theme fallback when site template missing",
			template: "section.html",
			theme:    "hyde",
			available: map[string]struct{}{
				"hyde/templates/section.html": {},
			},
			want: "hyde/templates/section.html",
		},
		{
			name:     "builtin fallback when site and theme missing",
			template: "404.html",
			theme:    "hyde",
			available: map[string]struct{}{
				"__zola_builtins/404.html": {},
			},
			want: "__zola_builtins/404.html",
		},
		{
			name:      "missing template returns error",
			template:  "taxonomy.html",
			theme:     "hyde",
			available: map[string]struct{}{},
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resolver := NewResolver(tc.theme)
			got, err := resolver.Resolve(tc.template, tc.available)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
