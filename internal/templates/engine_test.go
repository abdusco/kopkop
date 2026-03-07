package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngine_UndefinedIsStrict(t *testing.T) {
	t.Parallel()

	eng := NewEngine()
	require.NoError(t, eng.AddTemplate("index.html", `{{ missing_var }}`))

	_, err := eng.Render("index.html", map[string]any{"present": "ok"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "undefined variable")
}

func TestEngine_RelativeTemplateResolution(t *testing.T) {
	t.Parallel()

	eng := NewEngine()
	eng.EnableRelativeTemplateResolution()

	require.NoError(t, eng.AddTemplate("partials/header.html", `Header`))
	require.NoError(t, eng.AddTemplate("pages/home.html", `{% include "../partials/header.html" %}`))

	out, err := eng.Render("pages/home.html", nil)
	require.NoError(t, err)
	assert.Equal(t, "Header", out)
}

func TestDefaultTemplatePathJoin_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		parent string
		want   string
	}{
		{
			name:   "relative include from nested parent",
			input:  "../partials/header.html",
			parent: "pages/home/index.html",
			want:   "pages/partials/header.html",
		},
		{
			name:   "absolute style path drops leading slash",
			input:  "/layouts/base.html",
			parent: "pages/home.html",
			want:   "layouts/base.html",
		},
		{
			name:   "simple relative path in root parent",
			input:  "base.html",
			parent: "index.html",
			want:   "base.html",
		},
		{
			name:   "plain include resolves beside parent template",
			input:  "current_path.html",
			parent: "sample/templates/index.html",
			want:   "sample/templates/current_path.html",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DefaultTemplatePathJoin(tc.input, tc.parent)
			assert.Equal(t, tc.want, got)
		})
	}
}
