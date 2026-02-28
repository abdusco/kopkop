package linkcheck

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdusco/kopkop/internal/config"
	"github.com/abdusco/kopkop/internal/content"
)

func TestCheckExternalLinks_StatusAndAnchor(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte(`<html><body><h1 id="anchor">ok</h1></body></html>`))
		case "/bad":
			w.WriteHeader(http.StatusNotFound)
		default:
			_, _ = w.Write([]byte("ok"))
		}
	}))
	defer ts.Close()

	lib := content.NewLibrary()
	lib.Pages["a.md"] = &content.Page{ExternalLinks: []string{ts.URL + "/ok#anchor", ts.URL + "/bad"}}

	res := CheckExternalLinks(lib, config.LinkChecker{TimeoutSeconds: 2})
	require.Len(t, res, 2)

	got := map[string]Result{}
	for _, r := range res {
		got[r.URL] = r
	}
	assert.True(t, got[fmt.Sprintf("%s/ok#anchor", ts.URL)].OK)
	assert.False(t, got[fmt.Sprintf("%s/bad", ts.URL)].OK)
}

func TestCheckExternalLinks_UsesPersistentCache(t *testing.T) {
	t.Parallel()

	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(`<html><body>ok</body></html>`))
	}))
	defer ts.Close()

	lib := content.NewLibrary()
	lib.Pages["a.md"] = &content.Page{ExternalLinks: []string{ts.URL + "/ok"}}

	cacheFile := filepath.Join(t.TempDir(), "cache.json")
	cfg := config.LinkChecker{TimeoutSeconds: 2, CacheFile: cacheFile, UseCache: true}
	res1 := CheckExternalLinks(lib, cfg)
	require.Len(t, res1, 1)
	res2 := CheckExternalLinks(lib, cfg)
	require.Len(t, res2, 1)
	assert.Equal(t, int32(1), atomic.LoadInt32(&hits))
}
