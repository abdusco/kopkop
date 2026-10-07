package linkcheck

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/abdusco/kopkop/internal/config"
	"github.com/abdusco/kopkop/internal/content"
	"golang.org/x/net/html"
)

type htmlDocument struct {
	anchors map[string]bool
	links   []string
	base    string
}

func parseHTML(data []byte) (htmlDocument, error) {
	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return htmlDocument{}, err
	}
	doc := htmlDocument{anchors: map[string]bool{}}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				if a.Key == "id" || (n.Data == "a" && a.Key == "name") {
					doc.anchors[a.Val] = true
				}
				if n.Data == "base" && a.Key == "href" {
					if doc.base == "" {
						doc.base = a.Val
					}
					continue
				}
				if a.Key == "href" || a.Key == "src" || a.Key == "poster" || (n.Data == "object" && a.Key == "data") {
					doc.links = append(doc.links, a.Val)
				}
				if a.Key == "srcset" {
					doc.links = append(doc.links, srcsetURLs(a.Val)...)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return doc, nil
}

func srcsetURLs(input string) []string {
	var urls []string
	for input != "" {
		input = strings.TrimLeft(input, " \t\r\n\f,")
		if input == "" {
			break
		}
		end := strings.IndexAny(input, " \t\r\n\f")
		if end < 0 {
			end = len(input)
		}
		candidate := input[:end]
		input = input[end:]
		urls = append(urls, strings.TrimRight(candidate, ","))
		if strings.HasSuffix(candidate, ",") {
			continue
		}
		if comma := strings.IndexByte(input, ','); comma >= 0 {
			input = input[comma+1:]
		} else {
			break
		}
	}
	return urls
}

// CheckOutput validates actual published HTML rather than Markdown source nodes.
func CheckOutput(output fs.FS, baseURL string, cfg config.LinkChecker) ([]Result, error) {
	base, err := url.Parse(strings.TrimRight(baseURL, "/") + "/")
	if err != nil {
		return nil, err
	}
	files := map[string]bool{}
	docs := map[string]htmlDocument{}
	var names []string
	err = fs.WalkDir(output, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		files[name] = true
		if strings.EqualFold(path.Ext(name), ".html") {
			data, err := fs.ReadFile(output, name)
			if err != nil {
				return err
			}
			doc, err := parseHTML(data)
			if err != nil {
				return fmt.Errorf("parse %s: %w", name, err)
			}
			docs[name] = doc
			names = append(names, name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var results []Result
	var external []string
	seen := map[string]bool{}
	for _, name := range names {
		doc := docs[name]
		pagePath := name
		if path.Base(name) == "index.html" {
			pagePath = strings.TrimSuffix(name, "index.html")
		}
		pageURL := base.ResolveReference(&url.URL{Path: pagePath})
		if doc.base != "" {
			ref, err := url.Parse(doc.base)
			if err != nil {
				return nil, fmt.Errorf("%s: invalid base URL: %w", name, err)
			}
			pageURL = pageURL.ResolveReference(ref)
		}
		for _, raw := range doc.links {
			ref, err := url.Parse(raw)
			if err != nil {
				results = append(results, Result{URL: raw, Source: name, Internal: true, Error: err.Error()})
				continue
			}
			u := pageURL.ResolveReference(ref)
			if u.Scheme != "http" && u.Scheme != "https" {
				continue
			}
			if !strings.EqualFold(u.Host, base.Host) || !strings.EqualFold(u.Scheme, base.Scheme) {
				external = append(external, u.String())
				continue
			}
			key := name + "\n" + u.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			res := Result{URL: u.String(), Source: name, Internal: true}
			if !strings.HasPrefix(u.Path, base.Path) {
				res.Error = "target is outside the published base URL path"
			} else {
				target := strings.TrimPrefix(u.Path, base.Path)
				if target == "" || strings.HasSuffix(target, "/") {
					target += "index.html"
				}
				if !files[target] && files[path.Join(target, "index.html")] {
					target = path.Join(target, "index.html")
				}
				if !files[target] {
					res.Error = "output target not found"
				} else if u.Fragment != "" && docs[target].anchors != nil && !docs[target].anchors[u.Fragment] {
					res.Error = "anchor not found"
				} else {
					res.OK = true
				}
			}
			results = append(results, res)
		}
	}
	lib := content.NewLibrary()
	lib.Pages["output"] = &content.Page{ExternalLinks: external}
	ext, err := CheckExternalLinks(lib, cfg)
	results = append(results, ext...)
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].URL == results[j].URL {
			return results[i].Source < results[j].Source
		}
		return results[i].URL < results[j].URL
	})
	return results, err
}
