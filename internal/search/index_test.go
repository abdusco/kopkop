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

func TestBuildIndexForLanguages_FiltersLanguages(t *testing.T) {
	t.Parallel()

	lib := content.NewLibrary()
	lib.Pages["a.md"] = &content.Page{Meta: content.PageFrontMatter{Title: "EN"}, Permalink: "https://example.com/en/", Content: "en", Lang: "en"}
	lib.Pages["b.fr.md"] = &content.Page{Meta: content.PageFrontMatter{Title: "FR"}, Permalink: "https://example.com/fr/", Content: "fr", Lang: "fr"}

	out := t.TempDir()
	require.NoError(t, BuildIndexForLanguages(lib, out, "search_index.json", map[string]bool{"en": true}))

	require.FileExists(t, filepath.Join(out, "search_index.json"))
	require.NoFileExists(t, filepath.Join(out, "search_index.fr.js"))
	b, err := os.ReadFile(filepath.Join(out, "search_index.json"))
	require.NoError(t, err)
	require.Contains(t, string(b), "\"lang\":\"en\"")
	require.NotContains(t, string(b), "\"lang\":\"fr\"")
}

func TestStripTags_UsesVisibleTextNodesOnly(t *testing.T) {
	t.Parallel()

	in := `<h2 id="io-teereader">io.TeeReader<a class="zola-anchor" href="#io-teereader">🔗</a></h2><p>Hello <strong>world</strong> &amp; friends</p><script>var x = 1;</script>`
	out := stripTags(in)

	require.Equal(t, "io.TeeReader 🔗 Hello world & friends", out)
	require.NotContains(t, out, "id=")
	require.NotContains(t, out, "zola-anchor")
	require.NotContains(t, out, "var x")
}

func TestBuildIndex_StripsHTMLMarkupFromContent(t *testing.T) {
	t.Parallel()

	lib := content.NewLibrary()
	lib.Pages["a.md"] = &content.Page{
		Meta:      content.PageFrontMatter{Title: "Hello"},
		Permalink: "https://example.com/hello/",
		Content:   `<h1 id="title">Title<a href="#title">#</a></h1><p>Body <em>text</em></p>`,
		Lang:      "en",
	}

	outDir := t.TempDir()
	require.NoError(t, BuildIndex(lib, outDir, "search_index.json"))
	b, err := os.ReadFile(filepath.Join(outDir, "search_index.json"))
	require.NoError(t, err)

	var entries []Entry
	require.NoError(t, json.Unmarshal(b, &entries))
	require.Len(t, entries, 1)
	require.Equal(t, "Title # Body text", entries[0].Content)
	require.NotContains(t, entries[0].Content, "id=")
	require.NotContains(t, entries[0].Content, "href=")
}
