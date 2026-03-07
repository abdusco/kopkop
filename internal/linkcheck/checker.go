package linkcheck

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/abdusco/kopkop/internal/config"
	"github.com/abdusco/kopkop/internal/content"
	"github.com/samber/lo"
	"github.com/sourcegraph/conc/pool"
)

type Result struct {
	URL    string
	OK     bool
	Status int
	Error  string
}

func CheckExternalLinks(lib *content.Library, cfg config.LinkChecker) []Result {
	extLinks := lo.FlatMap(lo.Values(lib.Pages), func(pg *content.Page, _ int) []string {
		return pg.ExternalLinks
	})
	extLinks = lo.Uniq(extLinks)

	if len(extLinks) == 0 {
		return nil
	}

	client := &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second}
	cache := map[string]Result{}
	if cfg.UseCache {
		cache = loadCache(cfg.CacheFile)
	}

	pendingLinks := lo.Filter(extLinks, func(link string, _ int) bool {
		_, ok := cache[link]
		return !ok
	})

	if len(pendingLinks) > 0 {
		workers := pool.NewWithResults[Result]().WithMaxGoroutines(runtime.GOMAXPROCS(0) * 4)
		for _, link := range pendingLinks {
			workers.Go(func() Result {
				return checkURL(client, link, cfg)
			})
		}
		for _, res := range workers.Wait() {
			cache[res.URL] = res
		}
	}

	results := make([]Result, 0, len(extLinks))
	for _, link := range extLinks {
		if res, ok := cache[link]; ok {
			results = append(results, res)
		}
	}

	if cfg.UseCache {
		_ = saveCache(cfg.CacheFile, cache)
	}

	return results
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

	checkAnchor := strings.Contains(link, "#")
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
		anchor := link[strings.Index(link, "#")+1:]
		if anchor != "" && !strings.Contains(string(body), `id="`+anchor+`"`) {
			return Result{URL: link, OK: false, Status: resp.StatusCode, Error: "anchor not found"}
		}
	}

	return res
}

func loadCache(path string) map[string]Result {
	if strings.TrimSpace(path) == "" {
		return map[string]Result{}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return map[string]Result{}
	}
	out := map[string]Result{}
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]Result{}
	}
	return out
}

func saveCache(path string, data map[string]Result) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
