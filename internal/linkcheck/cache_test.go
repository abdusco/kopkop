package linkcheck

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/abdusco/kopkop/internal/config"
	"github.com/abdusco/kopkop/internal/content"
	"github.com/stretchr/testify/require"
)

func TestCacheExpirationAndRefresh(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		age                                    time.Duration
		initialStatus                          int
		refresh, zeroTTL, legacy, changePolicy bool
		wantHits                               int32
	}{
		{name: "fresh success reused", age: time.Minute, initialStatus: 200, wantHits: 1},
		{name: "expired success checked", age: 2 * time.Hour, initialStatus: 200, wantHits: 2},
		{name: "one tick before TTL reused", age: time.Hour - time.Nanosecond, initialStatus: 200, wantHits: 1},
		{name: "exactly TTL checked", age: time.Hour, initialStatus: 200, wantHits: 2},
		{name: "future timestamp checked", age: -time.Hour, initialStatus: 200, wantHits: 2},
		{name: "failures retried", age: time.Minute, initialStatus: 503, wantHits: 2},
		{name: "forced refresh", age: time.Minute, initialStatus: 200, refresh: true, wantHits: 2},
		{name: "zero TTL", age: time.Minute, initialStatus: 200, zeroTTL: true, wantHits: 2},
		{name: "legacy entry", initialStatus: 200, legacy: true, wantHits: 2},
		{name: "anchor policy changed", age: time.Minute, initialStatus: 200, changePolicy: true, wantHits: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The bubble's clock stands still while the test runs, so entry ages are exact.
			synctest.Test(t, func(t *testing.T) {
				var hits atomic.Int32
				status := tc.initialStatus
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if hits.Add(1) == 1 {
						w.WriteHeader(status)
					} else {
						w.WriteHeader(http.StatusNotFound)
					}
				}))
				defer ts.Close()
				lib := content.NewLibrary()
				lib.Pages["page.md"] = &content.Page{ExternalLinks: []string{ts.URL}}
				cfg := config.LinkChecker{TimeoutSeconds: 2, UseCache: true, CacheFile: filepath.Join(t.TempDir(), "cache.json"), CacheTTLSeconds: 3600}
				first, err := CheckExternalLinks(lib, cfg)
				require.NoError(t, err)
				require.Len(t, first, 1)
				cache, err := loadCache(cfg.CacheFile)
				require.NoError(t, err)
				entry := cache[ts.URL]
				entry.CheckedAt = time.Now().Add(-tc.age)
				if tc.legacy {
					entry.CheckedAt = time.Time{}
					entry.Policy = ""
				}
				cache[ts.URL] = entry
				require.NoError(t, saveCache(cfg.CacheFile, cache))
				cfg.Refresh = tc.refresh
				if tc.zeroTTL {
					cfg.CacheTTLSeconds = 0
				}
				if tc.changePolicy {
					cfg.SkipAnchorPrefixes = []string{ts.URL}
				}
				second, err := CheckExternalLinks(lib, cfg)
				require.NoError(t, err)
				require.Equal(t, tc.wantHits, hits.Load())
				if tc.wantHits == 1 {
					require.True(t, second[0].OK)
				} else {
					require.False(t, second[0].OK)
					require.Equal(t, http.StatusNotFound, second[0].Status)
				}
			})
		})
	}
}

func TestCacheErrorsAndRefreshRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, cachePath, cacheBody string
		refresh                    bool
		wantError                  string
	}{
		{"invalid JSON", "cache.json", "broken", false, "read link-check cache"},
		{"corrupt cache refreshed", "cache.json", "broken", true, ""},
		{"write error", "file/cache.json", "", false, "write link-check cache"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cachePath := filepath.Join(root, tc.cachePath)
			if tc.cacheBody != "" {
				require.NoError(t, os.WriteFile(cachePath, []byte(tc.cacheBody), 0o644))
			} else {
				require.NoError(t, os.WriteFile(filepath.Join(root, "file"), nil, 0o644))
			}
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			defer ts.Close()
			lib := content.NewLibrary()
			lib.Pages["page.md"] = &content.Page{ExternalLinks: []string{ts.URL}}
			// Skip reading in the write-error case so the failure reaches publication.
			cfg := config.LinkChecker{TimeoutSeconds: 2, UseCache: true, CacheFile: cachePath, CacheTTLSeconds: 3600, Refresh: tc.refresh || tc.cacheBody == ""}
			results, err := CheckExternalLinks(lib, cfg)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
				require.Len(t, results, 1)
				cache, err := loadCache(cachePath)
				require.NoError(t, err)
				require.True(t, cache[ts.URL].OK)
			}
		})
	}
}

func TestExternalLinkResultsAreSortedAndUnique(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()
	lib := content.NewLibrary()
	lib.Pages["z.md"] = &content.Page{ExternalLinks: []string{ts.URL + "/z", ts.URL + "/a"}}
	lib.Pages["a.md"] = &content.Page{ExternalLinks: []string{ts.URL + "/b", ts.URL + "/a"}}
	for range 10 {
		results, err := CheckExternalLinks(lib, config.LinkChecker{TimeoutSeconds: 2})
		require.NoError(t, err)
		require.Len(t, results, 3)
		require.Equal(t, ts.URL+"/a", results[0].URL)
		require.Equal(t, ts.URL+"/b", results[1].URL)
		require.Equal(t, ts.URL+"/z", results[2].URL)
	}
}
