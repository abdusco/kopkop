package linkcheck

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/abdusco/kopkop/internal/config"
	"github.com/abdusco/kopkop/internal/content"
	"github.com/samber/lo"
	"github.com/sourcegraph/conc/pool"
)

type Result struct {
	URL      string
	OK       bool
	Status   int
	Error    string
	Internal bool   `json:",omitempty"`
	Source   string `json:",omitempty"`
}

type cacheEntry struct {
	Result
	CheckedAt time.Time
	Policy    string
}

func CheckExternalLinks(lib *content.Library, cfg config.LinkChecker) ([]Result, error) {
	extLinks := lo.FlatMap(lo.Values(lib.Pages), func(pg *content.Page, _ int) []string {
		return pg.ExternalLinks
	})
	extLinks = lo.Uniq(extLinks)
	sort.Strings(extLinks)

	if len(extLinks) == 0 {
		return nil, nil
	}

	client := &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second}
	cache := map[string]cacheEntry{}
	if cfg.UseCache && !cfg.Refresh {
		var err error
		cache, err = loadCache(cfg.CacheFile)
		if err != nil {
			return nil, fmt.Errorf("read link-check cache %q: %w", cfg.CacheFile, err)
		}
	}
	policyBytes, _ := json.Marshal([]any{"html-anchors-v2", cfg.SkipAnchorPrefixes})
	policy := fmt.Sprintf("%x", sha256.Sum256(policyBytes))
	now := time.Now()
	ttl := time.Duration(cfg.CacheTTLSeconds) * time.Second

	pendingLinks := lo.Filter(extLinks, func(link string, _ int) bool {
		entry, ok := cache[link]
		age := now.Sub(entry.CheckedAt)
		return cfg.Refresh || !ok || !entry.OK || entry.URL != link || entry.Policy != policy || entry.CheckedAt.IsZero() || age < 0 || age >= ttl
	})

	if len(pendingLinks) > 0 {
		workers := pool.NewWithResults[Result]().WithMaxGoroutines(runtime.GOMAXPROCS(0) * 4)
		for _, link := range pendingLinks {
			workers.Go(func() Result {
				return checkURL(client, link, cfg)
			})
		}
		for _, res := range workers.Wait() {
			cache[res.URL] = cacheEntry{Result: res, CheckedAt: time.Now(), Policy: policy}
		}
	}

	results := make([]Result, 0, len(extLinks))
	for _, link := range extLinks {
		if res, ok := cache[link]; ok {
			results = append(results, res.Result)
		}
	}

	if cfg.UseCache {
		if err := saveCache(cfg.CacheFile, cache); err != nil {
			return results, fmt.Errorf("write link-check cache %q: %w", cfg.CacheFile, err)
		}
	}

	return results, nil
}

func checkURL(client *http.Client, link string, cfg config.LinkChecker) Result {
	req, err := http.NewRequest(http.MethodGet, link, nil)
	if err != nil {
		return Result{URL: link, OK: false, Error: err.Error()}
	}
	req.Header.Set("Accept", "text/html, */*")
	resp, err := client.Do(req)
	if err != nil {
		return Result{URL: link, OK: false, Error: err.Error()}
	}
	defer resp.Body.Close()

	ok := resp.StatusCode >= 200 && resp.StatusCode < 300 || resp.StatusCode == http.StatusNotModified
	res := Result{URL: link, OK: ok, Status: resp.StatusCode}

	if !ok {
		res.Error = fmt.Sprintf("non-success status %d", resp.StatusCode)
		return res
	}

	u, err := url.Parse(link)
	checkAnchor := err == nil && u.Fragment != ""
	for _, p := range cfg.SkipAnchorPrefixes {
		if strings.HasPrefix(link, p) {
			checkAnchor = false
			break
		}
	}

	if checkAnchor {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return Result{URL: link, OK: false, Status: resp.StatusCode, Error: readErr.Error()}
		}
		doc, parseErr := parseHTML(body)
		if parseErr != nil {
			return Result{URL: link, Status: resp.StatusCode, Error: parseErr.Error()}
		}
		if !doc.anchors[u.Fragment] {
			return Result{URL: link, OK: false, Status: resp.StatusCode, Error: "anchor not found"}
		}
	}

	return res
}

func loadCache(path string) (map[string]cacheEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]cacheEntry{}, nil
		}
		return nil, err
	}
	out := map[string]cacheEntry{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]cacheEntry{}
	}
	return out, nil
}

func saveCache(path string, data map[string]cacheEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".kopkop-linkcheck-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(b)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
