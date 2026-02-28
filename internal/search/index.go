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
	byLang := map[string][]Entry{}
	paths := make([]string, 0, len(lib.Pages))
	for p := range lib.Pages {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		pg := lib.Pages[p]
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
