package search

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestBuildIndexRejectsEscapingPaths(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"../sentinel", "/sentinel", `..\sentinel`, "link/sentinel"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			parent := t.TempDir()
			out := filepath.Join(parent, "public")
			outside := filepath.Join(parent, "outside")
			require.NoError(t, os.MkdirAll(out, 0o755))
			require.NoError(t, os.MkdirAll(outside, 0o755))
			sentinel := filepath.Join(outside, "sentinel")
			require.NoError(t, os.WriteFile(sentinel, []byte("keep me"), 0o644))
			require.NoError(t, os.Symlink("../outside", filepath.Join(out, "link")))
			require.Error(t, BuildIndex(content.NewLibrary(), out, name))
			data, err := os.ReadFile(sentinel)
			require.NoError(t, err)
			require.Equal(t, "keep me", string(data))
			require.NoFileExists(t, filepath.Join(parent, "sentinel"))
		})
	}
}

func TestStripTags_UsesVisibleTextNodesOnly(t *testing.T) {
	t.Parallel()

	in := `<h2 id="io-teereader">io.TeeReader<a class="kopkop-anchor" href="#io-teereader">🔗</a></h2><p>Hello <strong>world</strong> &amp; friends</p><script>var x = 1;</script>`
	out := stripTags(in)

	require.Equal(t, "io.TeeReader 🔗 Hello world & friends", out)
	require.NotContains(t, out, "id=")
	require.NotContains(t, out, "kopkop-anchor")
	require.NotContains(t, out, "var x")
}

func TestBuildIndex_StripsHTMLMarkupFromContent(t *testing.T) {
	t.Parallel()

	lib := content.NewLibrary()
	lib.Pages["a.md"] = &content.Page{
		Meta:      content.PageFrontMatter{Title: "Hello"},
		Permalink: "https://example.com/hello/",
		Content:   `<h1 id="title">Title<a href="#title">#</a></h1><p>Body <em>text</em></p>`,
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
