package search

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/abdusco/kopkop/internal/content"
)

func TestBuildIndex_WritesJSON(t *testing.T) {
	t.Parallel()

	lib := content.NewLibrary()
	lib.Pages["a.md"] = &content.Page{
		Meta:      content.PageFrontMatter{Title: "Hello"},
		Permalink: "https://example.com/hello/",
		Content:   "<p>Body</p>",
		Lang:      "en",
	}

	out := t.TempDir()
	require.NoError(t, BuildIndex(lib, out, "search_index.json"))
	b, err := os.ReadFile(filepath.Join(out, "search_index.json"))
	require.NoError(t, err)

	var entries []Entry
	require.NoError(t, json.Unmarshal(b, &entries))
	require.Len(t, entries, 1)
	require.Equal(t, "Hello", entries[0].Title)
	require.Equal(t, "https://example.com/hello/", entries[0].Permalink)
}

func TestBuildIndex_WritesPerLanguageJSForMultilingualSites(t *testing.T) {
	t.Parallel()

	lib := content.NewLibrary()
	lib.Pages["a.md"] = &content.Page{
		Meta:      content.PageFrontMatter{Title: "Hello"},
		Permalink: "https://example.com/hello/",
		Content:   "<p>Hello</p>",
		Lang:      "en",
	}
	lib.Pages["b.fr.md"] = &content.Page{
		Meta:      content.PageFrontMatter{Title: "Bonjour"},
		Permalink: "https://example.com/fr/bonjour/",
		Content:   "<p>Bonjour</p>",
		Lang:      "fr",
	}

	out := t.TempDir()
	require.NoError(t, BuildIndex(lib, out, "search_index.json"))

	require.FileExists(t, filepath.Join(out, "search_index.en.js"))
	require.FileExists(t, filepath.Join(out, "search_index.fr.js"))
	require.FileExists(t, filepath.Join(out, "elasticlunr.min.js"))
	require.NoFileExists(t, filepath.Join(out, "search_index.json"))

	en, err := os.ReadFile(filepath.Join(out, "search_index.en.js"))
	require.NoError(t, err)
	require.True(t, strings.Contains(string(en), "window.searchIndex[\"en\"]"))
}
