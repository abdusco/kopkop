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
