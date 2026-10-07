package server

import (
	"net"
	"net/http/httptest"
	"testing"

	"github.com/abdusco/kopkop/internal/config"
	"github.com/abdusco/kopkop/internal/filesystem"
	"github.com/abdusco/kopkop/internal/site"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreviewURL(t *testing.T) {
	for _, tc := range []struct {
		name, addr, configured, advertised, want string
		bad                                      bool
	}{
		{"actual port", "127.0.0.1:4321", "https://production.example", "", "http://127.0.0.1:4321/", false},
		{"wildcard", "0.0.0.0:4321", "https://production.example", "", "http://127.0.0.1:4321/", false},
		{"IPv6 wildcard", "[::]:4321", "https://production.example", "", "http://[::1]:4321/", false},
		{"IPv6", "[::1]:4321", "https://production.example/blog/", "", "http://[::1]:4321/blog/", false},
		{"advertised subpath", "127.0.0.1:4321", "https://production.example", "https://preview.example/site", "https://preview.example/site/", false},
		{"invalid origin", "127.0.0.1:4321", "https://production.example", "/site/", "", true},
		{"unclean path", "127.0.0.1:4321", "https://production.example", "http://preview.example/a/../b", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.BaseURL = tc.configured
			addr, err := net.ResolveTCPAddr("tcp", tc.addr)
			require.NoError(t, err)
			u, err := previewURL(&site.Site{Config: cfg}, ServeOptions{BaseURL: tc.advertised}, addr)
			if tc.bad {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, u.String())
		})
	}
}

func TestServeOutputHTTP(t *testing.T) {
	output := filesystem.NewMemoryFS()
	for name, data := range map[string]string{
		"index.html": "home", "post/index.html": "post", "404.html": "custom missing", "image.svg": "<svg></svg>", "image.png": "png", "font.woff2": "font", "file.txt": "0123456789",
	} {
		require.NoError(t, output.WriteFile(name, []byte(data), 0o644))
	}
	for _, tc := range []struct {
		name, method, url, header, value string
		status                           int
		body, ct, location               string
	}{
		{"root", "GET", "/blog/", "", "", 200, "home", "text/html; charset=utf-8", ""},
		{"clean directory", "GET", "/blog/post?q=x", "", "", 301, "", "", "/blog/post/?q=x"},
		{"directory index", "GET", "/blog/post/", "", "", 200, "post", "text/html; charset=utf-8", ""},
		{"HEAD", "HEAD", "/blog/post/", "", "", 200, "", "text/html; charset=utf-8", ""},
		{"missing", "GET", "/blog/missing", "", "", 404, "custom missing", "text/html; charset=utf-8", ""},
		{"file used as directory", "GET", "/blog/file.txt/", "", "", 404, "custom missing", "text/html; charset=utf-8", ""},
		{"HEAD missing", "HEAD", "/blog/missing", "", "", 404, "", "text/html; charset=utf-8", ""},
		{"SVG", "GET", "/blog/image.svg", "", "", 200, "<svg></svg>", "image/svg+xml", ""},
		{"PNG", "GET", "/blog/image.png", "", "", 200, "png", "image/png", ""},
		{"font", "GET", "/blog/font.woff2", "", "", 200, "font", "font/woff2", ""},
		{"range", "GET", "/blog/file.txt", "Range", "bytes=2-4", 206, "234", "text/plain; charset=utf-8", ""},
		{"invalid range", "GET", "/blog/file.txt", "Range", "bytes=99-100", 416, "", "", ""},
		{"POST", "POST", "/blog/file.txt", "", "", 405, "", "", ""},
		{"outside mount", "GET", "/file.txt", "", "", 404, "", "", ""},
		{"mount root", "GET", "/blog", "", "", 301, "", "", "/blog/"},
		{"traversal", "GET", "/blog/../../file.txt", "", "", 404, "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.url, nil)
			if tc.header != "" {
				r.Header.Set(tc.header, tc.value)
			}
			w := httptest.NewRecorder()
			serveOutput(output, "/blog", w, r)
			assert.Equal(t, tc.status, w.Code)
			if tc.body != "" || tc.method == "HEAD" {
				assert.Equal(t, tc.body, w.Body.String())
			}
			if tc.ct != "" {
				assert.Equal(t, tc.ct, w.Header().Get("Content-Type"))
			}
			if tc.location != "" {
				assert.Equal(t, tc.location, w.Header().Get("Location"))
			}
			if tc.method == "POST" {
				assert.Equal(t, "GET, HEAD", w.Header().Get("Allow"))
			}
		})
	}
}

func TestPreviewConditionalRequests(t *testing.T) {
	output := filesystem.NewMemoryFS()
	require.NoError(t, output.WriteFile("index.html", []byte("first"), 0o644))
	w := httptest.NewRecorder()
	serveOutput(output, "", w, httptest.NewRequest("GET", "/", nil))
	etag := w.Header().Get("ETag")
	require.NotEmpty(t, etag)
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("If-None-Match", etag)
	w = httptest.NewRecorder()
	serveOutput(output, "", w, r)
	assert.Equal(t, 304, w.Code)
	assert.Empty(t, w.Body.String())
	// A rebuild within the same second must still invalidate the old response.
	require.NoError(t, output.WriteFile("index.html", []byte("second"), 0o644))
	w = httptest.NewRecorder()
	serveOutput(output, "", w, r)
	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "second", w.Body.String())
	assert.NotEqual(t, etag, w.Header().Get("ETag"))
}
