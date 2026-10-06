package templates

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
