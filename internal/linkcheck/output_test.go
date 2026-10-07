package linkcheck

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/abdusco/kopkop/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutputLinks(t *testing.T) {
	for _, tc := range []struct {
		name, html, wantURL string
		ok                  bool
	}{
		{"relative clean URL", `<a href="post?x=1#caf%C3%A9">post</a>`, "https://example.com/blog/post?x=1#caf%C3%A9", true},
		{"single quotes and entity", `<a href='post/#a%26b'>post</a>`, "https://example.com/blog/post/#a%26b", true},
		{"legacy anchor", `<a href='post/#old'>post</a>`, "https://example.com/blog/post/#old", true},
		{"missing anchor", `<a href='post/#absent'>post</a>`, "https://example.com/blog/post/#absent", false},
		{"missing output", `<a href='missing/'>missing</a>`, "https://example.com/blog/missing/", false},
		{"outside subpath", `<a href='/elsewhere/'>outside</a>`, "https://example.com/elsewhere/", false},
		{"image", `<img src='image.png'>`, "https://example.com/blog/image.png", true},
		{"srcset", `<img srcset='data:image/png;base64,abc 1x, image.png 2x'>`, "https://example.com/blog/image.png", true},
		{"base URL", `<base href='/blog/post/'><a href='#old'>post</a>`, "https://example.com/blog/post/#old", true},
		{"protocol relative", `<a href='//example.com/blog/post/#old'>post</a>`, "https://example.com/blog/post/#old", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := fstest.MapFS{
				"index.html":      {Data: []byte(tc.html)},
				"post/index.html": {Data: []byte(`<h1 id='café'>hi</h1><div id='a&amp;b'></div><a name='old'></a>`)},
				"image.png":       {Data: []byte("image")},
			}
			results, err := CheckOutput(output, "https://example.com/blog/", config.LinkChecker{})
			require.NoError(t, err)
			require.Len(t, results, 1)
			assert.Equal(t, tc.wantURL, results[0].URL)
			assert.Equal(t, tc.ok, results[0].OK)
			assert.True(t, results[0].Internal)
			assert.Equal(t, "index.html", results[0].Source)
		})
	}
}

func TestOutputExternalLinksAndAnchors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<div id='a&amp;b'></div><a name='café'></a>`))
	}))
	defer srv.Close()
	output := fstest.MapFS{
		"index.html":         {Data: []byte(`<a href='` + srv.URL + `/#a%26b'>template</a>`)},
		"section/index.html": {Data: []byte(`<a href='` + srv.URL + `/#caf%C3%A9'>section</a><a href='` + srv.URL + `/#missing'>shortcode</a>`)},
	}
	results, err := CheckOutput(output, "https://example.com", config.LinkChecker{TimeoutSeconds: 2})
	require.NoError(t, err)
	require.Len(t, results, 3)
	for _, res := range results {
		assert.False(t, res.Internal)
		assert.Equal(t, res.URL != srv.URL+"/#missing", res.OK)
	}
}
