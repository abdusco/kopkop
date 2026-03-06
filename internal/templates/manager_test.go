package templates

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagerLoadAndRenderFallbacks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "themes", "hyde", "templates"), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(root, "themes", "hyde", "templates", "page.html"), []byte("theme-page"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "section.html"), []byte("site-section"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "themes", "hyde", "templates", "shortcodes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "themes", "hyde", "templates", "shortcodes", "pirate.html"), []byte("Arr"), 0o644))

	mgr, err := LoadManager(root, "hyde")
	require.NoError(t, err)

	out, err := mgr.Render("page.html", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "theme-page", out)

	out, err = mgr.Render("section.html", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "site-section", out)

	out, err = mgr.Render("404.html", map[string]any{})
	require.NoError(t, err)
	assert.Contains(t, out, "404")

	defs := mgr.ShortcodeDefinitions()
	def, ok := defs["pirate"]
	require.True(t, ok)
	assert.Equal(t, "hyde/templates/shortcodes/pirate.html", def.Template)
}

func TestManagerHelpers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "images"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "data.txt"), []byte("hello"), 0o644))
	img := image.NewRGBA(image.Rect(0, 0, 10, 5))
	for y := 0; y < 5; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	imgFile, err := os.Create(filepath.Join(root, "images", "sample.png"))
	require.NoError(t, err)
	require.NoError(t, png.Encode(imgFile, img))
	require.NoError(t, imgFile.Close())
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "test.txt"), []byte(`{{ "abc"|base64_encode }}|{{ "YWJj"|base64_decode }}|{{ "a1b2"|regex_replace("[0-9]", "") }}|{{ get_url("posts/hello") }}|{{ get_url(path="posts/hello", absolute=false) }}|{{ load_data("data.txt") }}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "test-url.html"), []byte(`<a href='{{ get_url(path="posts/hello") }}'>x</a>`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "test-image.txt"), []byte(`{% set md = get_image_metadata("images/sample.png") %}{{ md.width }}x{{ md.height }}|{{ resize_image("images/sample.png", 4, 2) }}`), 0o644))

	mgr, err := LoadManager(root, "")
	require.NoError(t, err)

	out, err := mgr.Render("test.txt", map[string]any{
		"config": map[string]any{"base_url": "https://example.com"},
	})
	require.NoError(t, err)
	assert.Equal(t, "YWJj|abc|ab|https://example.com/posts/hello|/posts/hello|hello", out)

	imgOut, err := mgr.Render("test-image.txt", map[string]any{})
	require.NoError(t, err)
	assert.Contains(t, imgOut, "10x5|")
	assert.Contains(t, imgOut, "/processed_images/")

	htmlOut, err := mgr.Render("test-url.html", map[string]any{
		"config": map[string]any{"base_url": "https://example.com"},
	})
	require.NoError(t, err)
	assert.Contains(t, htmlOut, "https://example.com/posts/hello")
	assert.NotContains(t, htmlOut, "&#x2F;")
}

func TestLoadData(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		data     map[string]string
		template string
		want     string
	}{
		{
			name:     "json object field access",
			data:     map[string]string{"data.json": `{"name":"Alice","age":30}`},
			template: `{{ load_data("data.json").name }}`,
			want:     "Alice",
		},
		{
			name:     "json array iteration",
			data:     map[string]string{"list.json": `["a","b","c"]`},
			template: `{% for x in load_data("list.json") %}{{ x }}{% endfor %}`,
			want:     "abc",
		},
		{
			name:     "toml field access",
			data:     map[string]string{"data.toml": "name = \"Bob\"\n"},
			template: `{{ load_data("data.toml").name }}`,
			want:     "Bob",
		},
		{
			name:     "yaml field access",
			data:     map[string]string{"data.yaml": "name: Carol\n"},
			template: `{{ load_data("data.yaml").name }}`,
			want:     "Carol",
		},
		{
			name:     "csv headers and records",
			data:     map[string]string{"data.csv": "id,name\n1,Alice\n2,Bob\n"},
			template: `{{ load_data("data.csv").headers | join(",") }}:{{ load_data("data.csv").records | length }}`,
			want:     "id,name:2",
		},
		{
			name:     "format=plain overrides json extension",
			data:     map[string]string{"raw.json": `{"x":1}`},
			template: `{{ load_data("raw.json", format="plain") }}`,
			want:     `{"x":1}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
			for name, content := range tc.data {
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
			}
			require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "t.txt"), []byte(tc.template), 0o644))

			mgr, err := LoadManager(root, "")
			require.NoError(t, err)

			out, err := mgr.Render("t.txt", map[string]any{})
			require.NoError(t, err)
			assert.Equal(t, tc.want, out)
		})
	}
}

func TestLoadURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		handler  http.HandlerFunc
		template func(serverURL string) string
		want     string
		wantErr  bool
	}{
		{
			name: "json via Content-Type header",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"name":"Alice"}`))
			},
			template: func(u string) string {
				return `{% set d = load_url(url="` + u + `") %}{{ d.name }}`
			},
			want: "Alice",
		},
		{
			name: "yaml via URL extension",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("name: Bob\n"))
			},
			template: func(u string) string {
				return `{% set d = load_url(url="` + u + `/data.yaml") %}{{ d.name }}`
			},
			want: "Bob",
		},
		{
			name: "format kwarg overrides Content-Type",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"x":1}`))
			},
			template: func(u string) string {
				return `{{ load_url(url="` + u + `", format="plain") }}`
			},
			want: `{"x":1}`,
		},
		{
			name: "custom header forwarded",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				b, _ := json.Marshal(map[string]string{"got": r.Header.Get("X-Test")})
				w.Write(b)
			},
			template: func(u string) string {
				return `{% set d = load_url(url="` + u + `", headers=["X-Test: hello"]) %}{{ d.got }}`
			},
			want: "hello",
		},
		{
			name: "POST with body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				w.Header().Set("Content-Type", "application/json")
				b, _ := json.Marshal(map[string]string{"received": string(body)})
				w.Write(b)
			},
			template: func(u string) string {
				return `{% set d = load_url(url="` + u + `", method="POST", body="hello") %}{{ d.received }}`
			},
			want: "hello",
		},
		{
			name: "non-200 returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "not found", http.StatusNotFound)
			},
			template: func(u string) string {
				return `{{ load_url(url="` + u + `") }}`
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
			tpl := tc.template(srv.URL)
			require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "t.txt"), []byte(tpl), 0o644))

			mgr, err := LoadManager(root, "")
			require.NoError(t, err)

			out, err := mgr.Render("t.txt", map[string]any{})
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.want, out)
			}
		})
	}
}

func TestLoadURL_Cache(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"n":1}`))
	}))
	defer srv.Close()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
	// Call load_url twice with the same URL in one template render
	tpl := `{{ load_url(url="` + srv.URL + `").n }}|{{ load_url(url="` + srv.URL + `").n }}`
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "t.txt"), []byte(tpl), 0o644))

	mgr, err := LoadManager(root, "")
	require.NoError(t, err)

	out, err := mgr.Render("t.txt", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "1|1", out)
	assert.Equal(t, int32(1), hits.Load(), "server should be hit only once due to caching")
}

func TestLookupHelpers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "lookup.txt"), []byte(`{% set p = get_page(path="a.md") %}{% set s = get_section(path="blog/_index.md") %}{% set tx = get_taxonomy(kind="tags") %}{{ p.title }}|{{ s.title }}|{{ tx.name }}|{{ get_taxonomy_url(kind="tags", term="Go Lang") }}`), 0o644))

	mgr, err := LoadManager(root, "")
	require.NoError(t, err)

	out, err := mgr.Render("lookup.txt", map[string]any{
		"__pages": map[string]any{
			"a.md": map[string]any{"title": "PageA"},
		},
		"__sections": map[string]any{
			"blog/_index.md": map[string]any{"title": "Blog"},
		},
		"__taxonomies": map[string]any{
			"tags": map[string]any{"name": "tags", "terms": map[string]any{}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "PageA|Blog|tags|/tags/go-lang/", out)
}

func TestGetURL_UsesConfigLinkStrategy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "url.txt"), []byte(`{{ get_url("posts/hello") }}`), 0o644))

	mgr, err := LoadManager(root, "")
	require.NoError(t, err)

	out, err := mgr.Render("url.txt", map[string]any{
		"config": map[string]any{"base_url": "https://example.com", "link_strategy": "relative"},
	})
	require.NoError(t, err)
	assert.Equal(t, "/posts/hello", out)
}

func TestNormalizeTemplateSyntax_NamedEndTags(t *testing.T) {
	t.Parallel()

	in := "{% macro twice(str) %}{{str}}{% endmacro twice %}\n{% block a %}x{% endblock a %}\n{{ macros::twice(str=\"hey\") }}"
	out := normalizeTemplateSyntax(in)
	assert.Contains(t, out, "{% endmacro %}")
	assert.Contains(t, out, "{% endblock %}")
	assert.Contains(t, out, "{{ macros.twice(str=\"hey\") }}")
	assert.NotContains(t, out, "endmacro twice")
	assert.NotContains(t, out, "endblock a")
	assert.NotContains(t, out, "macros::twice")
}
