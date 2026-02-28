package search

import (
	"encoding/json"
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
	entries := make([]Entry, 0, len(lib.Pages))
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
		entries = append(entries, Entry{
			Title:     pg.Meta.Title,
			Permalink: pg.Permalink,
			Summary:   summary,
			Content:   stripTags(pg.Content),
			Lang:      pg.Lang,
		})
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

func stripTags(s string) string {
	replacer := strings.NewReplacer("<", " ", ">", " ", "\n", " ", "\t", " ")
	out := replacer.Replace(s)
	return strings.Join(strings.Fields(out), " ")
}
