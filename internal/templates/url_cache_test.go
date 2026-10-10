package templates

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestURLCacheAcrossTemplateReloads(t *testing.T) {
	for _, tc := range []struct {
		name      string
		reuse     bool
		expire    bool
		disable   bool
		changeTTL bool
		wantHits  int32
	}{
		{name: "ordinary builds fetch again", wantHits: 2},
		{name: "preview builds reuse responses", reuse: true, wantHits: 1},
		{name: "expired preview responses refresh", reuse: true, expire: true, wantHits: 2},
		{name: "disabling preview cache fetches again", reuse: true, disable: true, wantHits: 2},
		{name: "changing duration fetches again", reuse: true, changeTTL: true, wantHits: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				_, _ = w.Write([]byte(`{"n":1}`))
			}))
			defer srv.Close()
			mfs := newMemoryFS(map[string][]byte{"templates/t.txt": []byte(`{{ load_url(url="` + srv.URL + `", format="json").n }}`)})
			previous, err := LoadManagerFS(mfs, mfs, "")
			require.NoError(t, err)
			if tc.reuse {
				previous.ReuseURLCache(previous, 5*time.Minute)
				previous.ConfigureHelpers()
			}
			out, err := previous.Render("t.txt", nil)
			require.NoError(t, err)
			require.Equal(t, "1", out)
			if tc.expire {
				previous.urlCache.mu.Lock()
				for _, load := range previous.urlCache.loads {
					load.expiresAt = time.Now().Add(-time.Second)
				}
				previous.urlCache.mu.Unlock()
			}
			next, err := LoadManagerFS(mfs, mfs, "")
			require.NoError(t, err)
			if tc.reuse {
				ttl := 5 * time.Minute
				if tc.disable {
					ttl = 0
				} else if tc.changeTTL {
					ttl = time.Minute
				}
				next.ReuseURLCache(previous, ttl)
			}
			next.ConfigureHelpers()
			out, err = next.Render("t.txt", nil)
			require.NoError(t, err)
			require.Equal(t, "1", out)
			require.Equal(t, tc.wantHits, hits.Load())
		})
	}
}

func TestURLCacheIdentityAndCoalescing(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(r.Header.Get("Authorization")))
	}))
	defer srv.Close()
	cache := &responseCache{}
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Go(func() {
			req, err := http.NewRequest("POST", srv.URL, strings.NewReader("body"))
			require.NoError(t, err)
			req.Header.Set("authorization", "one")
			entry, err := cache.Load(req, "body")
			assert.NoError(t, err)
			assert.Equal(t, "one", string(entry.body))
		})
	}
	wg.Wait()
	assert.EqualValues(t, 1, hits.Load())
	req, err := http.NewRequest("POST", srv.URL, strings.NewReader("body"))
	require.NoError(t, err)
	req.Header.Set("Authorization", "two")
	entry, err := cache.Load(req, "body")
	require.NoError(t, err)
	assert.Equal(t, "two", string(entry.body))
	assert.EqualValues(t, 2, hits.Load())
}

func TestURLFormat(t *testing.T) {
	u, err := url.Parse("https://example.com/data.yaml?format=csv")
	require.NoError(t, err)
	for _, tc := range []struct{ explicit, ct, want string }{
		{"plain", "application/json", "plain"},
		{"", "application/json; charset=utf-8", "json"},
		{"", "application/vnd.api+json", "json"},
		{"", "Application/JSON", "json"},
		{"", "text/x-yaml", "yaml"},
		{"", "application/octet-stream", "yaml"},
	} {
		t.Run(tc.want, func(t *testing.T) { assert.Equal(t, tc.want, urlFormat(tc.explicit, u, tc.ct)) })
	}
}

func TestLoadURLConflictingFormatOnCacheHit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"n":1}`))
	}))
	defer srv.Close()
	mfs := newMemoryFS(map[string][]byte{"templates/t.txt": []byte(`{{ load_url(url="` + srv.URL + `/data.csv").n }}|{{ load_url(url="` + srv.URL + `/data.csv").n }}`)})
	mgr, err := LoadManagerFS(mfs, mfs, "")
	require.NoError(t, err)
	out, err := mgr.Render("t.txt", nil)
	require.NoError(t, err)
	assert.Equal(t, "1|1", out)
}
