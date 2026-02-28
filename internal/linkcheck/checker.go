package linkcheck

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/abdusco/kopkop/internal/config"
	"github.com/abdusco/kopkop/internal/content"
)

type Result struct {
	URL    string
	OK     bool
	Status int
	Error  string
}

func CheckExternalLinks(lib *content.Library, cfg config.LinkChecker) []Result {
	unique := map[string]struct{}{}
	for _, p := range lib.Pages {
		for _, link := range p.ExternalLinks {
			unique[link] = struct{}{}
		}
	}

	client := &http.Client{Timeout: time.Duration(cfg.TimeoutSeconds) * time.Second}
	cache := map[string]Result{}
	var mu sync.Mutex
	results := []Result{}

	for link := range unique {
		res := checkURL(client, link, cfg)
		mu.Lock()
		cache[link] = res
		results = append(results, res)
		mu.Unlock()
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
