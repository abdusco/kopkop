package templates

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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

	"github.com/abdusco/kopkop/internal/filesystem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetURLCachebustSources(t *testing.T) {
	for _, tc := range []struct {
		name      string
		source    map[string][]byte
		output    map[string][]byte
		colocated map[string]string
		want      string
	}{
		{"static source", map[string][]byte{"static/asset.js": []byte("static")}, nil, nil, "static"},
		{"existing output precedence", map[string][]byte{"static/asset.js": []byte("static")}, map[string][]byte{"asset.js": []byte("output")}, nil, "output"},
		{"colocated source over stale output", map[string][]byte{"content/bundle/asset.js": []byte("fresh")}, map[string][]byte{"asset.js": []byte("stale")}, map[string]string{"asset.js": "content/bundle/asset.js"}, "fresh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.source["templates/url.txt"] = []byte(`{{ get_url(path="asset.js", cachebust=true, absolute=false) }}`)
			mgr, err := LoadManagerFS(newMemoryFS(tc.source), newMemoryFS(tc.output), "")
			require.NoError(t, err)
			mgr.ColocatedAssets = tc.colocated
			mgr.ConfigureHelpers()
			out, err := mgr.Render("url.txt", nil)
			require.NoError(t, err)
			h := sha256.Sum256([]byte(tc.want))
			require.Equal(t, fmt.Sprintf("/asset.js?h=%x", h[:10]), out)
		})
	}
}

func TestManagerLoadAndRenderFallbacks(t *testing.T) {
	t.Parallel()

	fys := newMemoryFS(map[string][]byte{
		"templates/section.html":                       []byte("site-section"),
		"themes/hyde/templates/page.html":              []byte("theme-page"),
		"themes/hyde/templates/shortcodes/pirate.html": []byte("Arr"),
	})

	mgr, err := LoadManagerFS(fys, fys, "hyde")
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

func TestManagerReportsResolvedTemplateErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		files    map[string][]byte
		resolved string
	}{
		{name: "site override", files: map[string][]byte{"templates/page.html": []byte("{{ broken() }}"), "themes/demo/templates/page.html": []byte("OK")}, resolved: "page.html"},
		{name: "theme override", files: map[string][]byte{"themes/demo/templates/page.html": []byte("{{ broken() }}")}, resolved: "page.html"},
		{name: "explicit theme name", files: map[string][]byte{"themes/demo/templates/page.html": []byte("{{ broken() }}")}, resolved: "demo/templates/page.html"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fys := newMemoryFS(tc.files)
			mgr, err := LoadManagerFS(fys, fys, "demo")
			require.NoError(t, err)
			out, err := mgr.Render(tc.resolved, nil)
			require.Empty(t, out)
			require.ErrorContains(t, err, `render template "`+tc.resolved+`"`)
			require.ErrorContains(t, err, "unknown function")
			require.ErrorContains(t, err, "line 1")
		})
	}
}

func TestTemplateFileHelpersRejectEscapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		template string
	}{
		{name: "load data traversal", template: `{{ load_data("../private") }}`},
		{name: "load data symlink", template: `{{ load_data("link") }}`},
		{name: "image metadata traversal", template: `{{ get_image_metadata("../private") }}`},
		{name: "image metadata symlink", template: `{{ get_image_metadata("link") }}`},
		{name: "resize traversal", template: `{{ resize_image("../private", 1, 1) }}`},
		{name: "resize symlink", template: `{{ resize_image("link", 1, 1) }}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parent := t.TempDir()
			root := filepath.Join(parent, "site")
			require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(parent, "private"), mustPNG(2, 2), 0o644))
			require.NoError(t, os.Symlink("../private", filepath.Join(root, "link")))
			require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "probe.txt"), []byte(tc.template), 0o644))
			mgr, err := LoadManagerFS(filesystem.NewDiskFS(root), filesystem.NewDiskFS(filepath.Join(root, "public")), "")
			require.NoError(t, err)
			_, err = mgr.Render("probe.txt", nil)
			require.Error(t, err)
			require.NoDirExists(t, filepath.Join(root, "public"))
		})
	}
}

func TestManagerHelpers(t *testing.T) {
	t.Parallel()

	fys := newMemoryFS(map[string][]byte{
		"data.txt":                 []byte("hello"),
		"images/sample.png":        mustPNG(10, 5),
		"templates/test.txt":       []byte(`{{ "abc"|base64_encode }}|{{ "YWJj"|base64_decode }}|{{ "a1b2"|regex_replace("[0-9]", "") }}|{{ get_url("posts/hello") }}|{{ get_url(path="posts/hello", absolute=false) }}|{{ load_data("data.txt") }}`),
		"templates/test-url.html":  []byte(`<a href='{{ get_url(path="posts/hello") }}'>x</a>`),
		"templates/test-image.txt": []byte(`{% set md = get_image_metadata("images/sample.png") %}{{ md.width }}x{{ md.height }}`),
	})

	mgr, err := LoadManagerFS(fys, fys, "")
	require.NoError(t, err)

	out, err := mgr.Render("test.txt", map[string]any{
		"config": map[string]any{"base_url": "https://example.com"},
	})
	require.NoError(t, err)
	assert.Equal(t, "YWJj|abc|ab|https://example.com/posts/hello|/posts/hello|hello", out)

	imgOut, err := mgr.Render("test-image.txt", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "10x5", imgOut)

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
		{name: "json object field access", data: map[string]string{"data.json": `{"name":"Alice","age":30}`}, template: `{{ load_data("data.json").name }}`, want: "Alice"},
		{name: "path keyword", data: map[string]string{"data.json": `{"name":"Alice"}`}, template: `{{ load_data(path="data.json").name }}`, want: "Alice"},
		{name: "json array iteration", data: map[string]string{"list.json": `["a","b","c"]`}, template: `{% for x in load_data("list.json") %}{{ x }}{% endfor %}`, want: "abc"},
		{name: "toml field access", data: map[string]string{"data.toml": "name = \"Bob\"\n"}, template: `{{ load_data("data.toml").name }}`, want: "Bob"},
		{name: "yaml field access", data: map[string]string{"data.yaml": "name: Carol\n"}, template: `{{ load_data("data.yaml").name }}`, want: "Carol"},
		{name: "csv headers and records", data: map[string]string{"data.csv": "id,name\n1,Alice\n2,Bob\n"}, template: `{{ load_data("data.csv").headers | join(",") }}:{{ load_data("data.csv").records | length }}`, want: "id,name:2"},
		{name: "format=plain overrides json extension", data: map[string]string{"raw.json": `{"x":1}`}, template: `{{ load_data("raw.json", format="plain") }}`, want: `{"x":1}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files := map[string][]byte{"templates/t.txt": []byte(tc.template)}
			for name, content := range tc.data {
				files[name] = []byte(content)
			}
			mfs := newMemoryFS(files)
			mgr, err := LoadManagerFS(mfs, mfs, "")
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
		{name: "json via Content-Type header", handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"name":"Alice"}`))
		}, template: func(u string) string { return `{% set d = load_url(url="` + u + `") %}{{ d.name }}` }, want: "Alice"},
		{name: "yaml via URL extension", handler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("name: Bob\n")) }, template: func(u string) string { return `{% set d = load_url(url="` + u + `/data.yaml") %}{{ d.name }}` }, want: "Bob"},
		{name: "format kwarg overrides Content-Type", handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"x":1}`))
		}, template: func(u string) string { return `{{ load_url(url="` + u + `", format="plain") }}` }, want: `{"x":1}`},
		{name: "custom header forwarded", handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			b, _ := json.Marshal(map[string]string{"got": r.Header.Get("X-Test")})
			_, _ = w.Write(b)
		}, template: func(u string) string {
			return `{% set d = load_url(url="` + u + `", headers=["X-Test: hello"]) %}{{ d.got }}`
		}, want: "hello"},
		{name: "POST with body", handler: func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			b, _ := json.Marshal(map[string]string{"received": string(body)})
			_, _ = w.Write(b)
		}, template: func(u string) string {
			return `{% set d = load_url(url="` + u + `", method="POST", body="hello") %}{{ d.received }}`
		}, want: "hello"},
		{name: "non-200 returns error", handler: func(w http.ResponseWriter, r *http.Request) { http.Error(w, "not found", http.StatusNotFound) }, template: func(u string) string { return `{{ load_url(url="` + u + `") }}` }, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			mfs := newMemoryFS(map[string][]byte{"templates/t.txt": []byte(tc.template(srv.URL))})
			mgr, err := LoadManagerFS(mfs, mfs, "")
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
		_, _ = w.Write([]byte(`{"n":1}`))
	}))
	defer srv.Close()

	mfs := newMemoryFS(map[string][]byte{
		"templates/t.txt": []byte(`{{ load_url(url="` + srv.URL + `").n }}|{{ load_url(url="` + srv.URL + `").n }}`),
	})
	mgr, err := LoadManagerFS(mfs, mfs, "")
	require.NoError(t, err)

	out, err := mgr.Render("t.txt", map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "1|1", out)
	assert.Equal(t, int32(1), hits.Load(), "server should be hit only once due to caching")
}

func TestLookupHelpers(t *testing.T) {
	t.Parallel()

	mfs := newMemoryFS(map[string][]byte{
		"templates/lookup.txt": []byte(`{% set p = get_page(path="a.md") %}{% set s = get_section(path="blog/_index.md") %}{% set tx = get_taxonomy(kind="tags") %}{{ p.title }}|{{ s.title }}|{{ tx.name }}|{{ get_taxonomy_url(kind="tags", term="Go Lang") }}`),
	})
	mgr, err := LoadManagerFS(mfs, mfs, "")
	require.NoError(t, err)

	out, err := mgr.Render("lookup.txt", map[string]any{
		"__pages":      map[string]any{"a.md": map[string]any{"title": "PageA"}},
		"__sections":   map[string]any{"blog/_index.md": map[string]any{"title": "Blog"}},
		"__taxonomies": map[string]any{"tags": map[string]any{"name": "tags", "terms": map[string]any{}}},
	})
	require.NoError(t, err)
	assert.Equal(t, "PageA|Blog|tags|/tags/go-lang/", out)
}

func TestGetURL_UsesConfigLinkStrategy(t *testing.T) {
	t.Parallel()

	mfs := newMemoryFS(map[string][]byte{
		"templates/url.txt": []byte(`{{ get_url("posts/hello") }}`),
	})
	mgr, err := LoadManagerFS(mfs, mfs, "")
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

func mustPNG(w int, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func newMemoryFS(files map[string][]byte) *filesystem.MemoryFS {
	m := filesystem.NewMemoryFS()
	for name, data := range files {
		_ = m.WriteFile(name, data, 0o644)
	}
	return m
}

func TestResizeImageHelper(t *testing.T) {
	t.Parallel()

	fys := newMemoryFS(map[string][]byte{
		"images/wide.png":          mustPNG(40, 20),
		"templates/fit.txt":        []byte(`{% set r = resize_image(path="images/wide.png", width=10, op="fit_width") %}{{ r.width }}x{{ r.height }} {{ r.url }}`),
		"templates/positional.txt": []byte(`{{ resize_image("images/wide.png", 8, 4, op="scale").url }}`),
		"templates/meta.txt":       []byte(`{{ get_image_metadata(path="images/wide.png").width }}`),
	})
	mgr, err := LoadManagerFS(fys, fys, "")
	require.NoError(t, err)
	ctx := map[string]any{"config": map[string]any{"base_url": "https://example.com/blog/"}}

	out, err := mgr.Render("fit.txt", ctx)
	require.NoError(t, err)
	assert.Regexp(t, `^10x5 https://example\.com/blog/processed_images/[0-9a-f]{64}-10x5\.png$`, out)

	out, err = mgr.Render("positional.txt", ctx)
	require.NoError(t, err)
	assert.Regexp(t, `^https://example\.com/blog/processed_images/[0-9a-f]{64}-8x4\.png$`, out)

	out, err = mgr.Render("meta.txt", ctx)
	require.NoError(t, err)
	assert.Equal(t, "40", out)
}

func TestNormalizeTemplateSyntax(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, in, want string }{
		{"macro call in expression", `{{ macros::input(name="a") }}`, `{{ macros.input(name="a") }}`},
		{"macro call in tag", `{% set x = m::f(1) %}`, `{% set x = m.f(1) %}`},
		{"named end tag", `{% endmacro input %}`, `{% endmacro %}`},
		{"prose untouched", `<p>Vec::new( and std::string(x)</p>`, `<p>Vec::new( and std::string(x)</p>`},
		{"string literal untouched", `{{ "a::b(" }} {{ m::f() }}`, `{{ "a::b(" }} {{ m.f() }}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, normalizeTemplateSyntax(tc.in))
		})
	}
}

func TestEscapeFilterUsesTeraEntities(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		template string
		want     string
	}{
		{name: "slash and quotes", template: `{{ v | escape }}`, want: `&lt;a href=&quot;&#x2F;x&quot;&gt;it&#x27;s &amp;&lt;&#x2F;a&gt;`},
		{name: "short alias", template: `{{ v | e }}`, want: `&lt;a href=&quot;&#x2F;x&quot;&gt;it&#x27;s &amp;&lt;&#x2F;a&gt;`},
		{name: "plain slash outside escape stays", template: `{{ "a/b" }}`, want: `a/b`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fys := newMemoryFS(map[string][]byte{"templates/probe.html": []byte(tc.template)})
			mgr, err := LoadManagerFS(fys, fys, "")
			require.NoError(t, err)
			out, err := mgr.Render("probe.html", map[string]any{"v": `<a href="/x">it's &</a>`})
			require.NoError(t, err)
			require.Equal(t, tc.want, out)
		})
	}
}
