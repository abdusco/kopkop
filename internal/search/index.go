package search

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/abdusco/kopkop/internal/content"
)

type Entry struct {
	Title     string `json:"title"`
	Permalink string `json:"permalink"`
	Summary   string `json:"summary"`
	Content   string `json:"content"`
	Lang      string `json:"lang"`
}

func BuildIndex(lib *content.Library, outputPath string, filename string) error {
	return BuildIndexForLanguages(lib, outputPath, filename, nil)
}

func BuildIndexForLanguages(lib *content.Library, outputPath string, filename string, enabledLangs map[string]bool) error {
	byLang := map[string][]Entry{}
	permalinkSeen := map[string]struct{}{}
	paths := make([]string, 0, len(lib.Pages))
	for p := range lib.Pages {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		pg := lib.Pages[p]
		if len(enabledLangs) > 0 && !enabledLangs[pg.Lang] {
			continue
		}
		summary := ""
		if pg.Summary != nil {
			summary = *pg.Summary
		}
		byLang[pg.Lang] = append(byLang[pg.Lang], Entry{
			Title:     pg.Meta.Title,
			Permalink: pg.Permalink,
			Summary:   summary,
			Content:   stripTags(pg.Content),
			Lang:      pg.Lang,
		})
		permalinkSeen[pg.Permalink] = struct{}{}
	}

	sectionPaths := make([]string, 0, len(lib.Sections))
	for p := range lib.Sections {
		sectionPaths = append(sectionPaths, p)
	}
	sort.Strings(sectionPaths)
	for _, p := range sectionPaths {
		sec := lib.Sections[p]
		if len(enabledLangs) > 0 && !enabledLangs[sec.Lang] {
			continue
		}
		if _, exists := permalinkSeen[sec.Permalink]; exists {
			continue
		}
		byLang[sec.Lang] = append(byLang[sec.Lang], Entry{
			Title:     sec.Meta.Title,
			Permalink: sec.Permalink,
			Summary:   "",
			Content:   stripTags(sec.Content),
			Lang:      sec.Lang,
		})
		permalinkSeen[sec.Permalink] = struct{}{}
	}

	if len(enabledLangs) > 0 {
		defaultRoot := lib.Sections["_index.md"]
		if defaultRoot != nil {
			defaultLang := defaultRoot.Lang
			base := strings.TrimRight(defaultRoot.Permalink, "/")
			langs := make([]string, 0, len(enabledLangs))
			for lang := range enabledLangs {
				langs = append(langs, lang)
			}
			sort.Strings(langs)
			for _, lang := range langs {
				if !enabledLangs[lang] || lang == defaultLang {
					continue
				}
				perm := base + "/" + lang + "/"
				if _, exists := permalinkSeen[perm]; exists {
					continue
				}
				byLang[lang] = append(byLang[lang], Entry{
					Title:     "",
					Permalink: perm,
					Summary:   "",
					Content:   "",
					Lang:      lang,
				})
				permalinkSeen[perm] = struct{}{}
			}
		}
	}

	if len(byLang) <= 1 {
		entries := make([]Entry, 0, len(lib.Pages))
		for _, entry := range byLang {
			entries = append(entries, entry...)
		}
		b, err := json.Marshal(entries)
		if err != nil {
			return err
		}
		dst := filepath.Join(outputPath, filename)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	}

	langs := make([]string, 0, len(byLang))
	for lang := range byLang {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	for _, lang := range langs {
		b, err := json.Marshal(byLang[lang])
		if err != nil {
			return err
		}
		js := fmt.Sprintf("window.searchIndex = window.searchIndex || {};\nwindow.searchIndex[%q] = %s;\n", lang, string(b))
		dst := filepath.Join(outputPath, "search_index."+lang+".js")
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, []byte(js), 0o644); err != nil {
			return err
		}
	}

	elasticlunrPath := filepath.Join(outputPath, "elasticlunr.min.js")
	if err := os.WriteFile(elasticlunrPath, []byte("window.elasticlunr = window.elasticlunr || {};\n"), 0o644); err != nil {
		return err
	}
	return nil
}

func stripTags(s string) string {
	replacer := strings.NewReplacer("<", " ", ">", " ", "\n", " ", "\t", " ")
	out := replacer.Replace(s)
	return strings.Join(strings.Fields(out), " ")
}
