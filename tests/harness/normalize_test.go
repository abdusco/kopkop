package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeByExt_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ext  string
		in   string
		want string
	}{
		{
			name: "json canonical",
			ext:  ".json",
			in:   `{"b":2,"a":1}`,
			want: `{"a":1,"b":2}`,
		},
		{
			name: "html whitespace",
			ext:  ".html",
			in:   "<div>  hello </div>\n\n<span> x </span>",
			want: "<div> hello </div><span> x </span>",
		},
		{
			name: "plain text trim",
			ext:  ".txt",
			in:   "  hello\n",
			want: "hello",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeByExt(tc.ext, []byte(tc.in))
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(got))
		})
	}
}

func TestCompareDirectories_NormalizedContent(t *testing.T) {
	t.Parallel()

	left := t.TempDir()
	right := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(left, "index.html"), []byte("<div> hi </div>\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(right, "index.html"), []byte("<div>   hi   </div>"), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(left, "data.json"), []byte(`{"a":1,"b":2}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(right, "data.json"), []byte(`{"b":2,"a":1}`), 0o644))

	diffs, err := CompareDirectories(left, right, nil)
	require.NoError(t, err)
	assert.Empty(t, diffs)
}

func TestCompareDirectories_DetectsMissingAndDiff(t *testing.T) {
	t.Parallel()

	left := t.TempDir()
	right := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(left, "same.txt"), []byte("same"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(right, "same.txt"), []byte("same"), 0o644))

	require.NoError(t, os.WriteFile(filepath.Join(left, "only-left.txt"), []byte("left"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(right, "different.txt"), []byte("right"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(left, "different.txt"), []byte("left"), 0o644))

	diffs, err := CompareDirectories(left, right, nil)
	require.NoError(t, err)

	assert.Len(t, diffs, 2)
	assert.Equal(t, "content differs", diffs[0].Reason)
	assert.Equal(t, "different.txt", diffs[0].Path)
	assert.Equal(t, "missing in right", diffs[1].Reason)
	assert.Equal(t, "only-left.txt", diffs[1].Path)
}

func TestCompareDirectories_IgnoresGlobPatterns(t *testing.T) {
	t.Parallel()

	left := t.TempDir()
	right := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(left, "a.html"), []byte("one"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(right, "a.html"), []byte("two"), 0o644))

	diffs, err := CompareDirectories(left, right, []string{"*.html"})
	require.NoError(t, err)
	assert.Empty(t, diffs)
}

func TestNormalizeHTML_IgnoresTranslationOrderOnly(t *testing.T) {
	t.Parallel()

	a := `<p>Intro</p>
Translated in fr: Bonjour
<br><br>
<p>Salut</p>
Translated in en: Hello
<br><br>
<p>Hi</p>`
	b := `<p>Intro</p>
Translated in en: Hello
<br><br>
<p>Hi</p>
Translated in fr: Bonjour
<br><br>
<p>Salut</p>`

	na, err := NormalizeByExt(".html", []byte(a))
	require.NoError(t, err)
	nb, err := NormalizeByExt(".html", []byte(b))
	require.NoError(t, err)
	assert.Equal(t, string(na), string(nb))
}

func TestNormalizeHTML_StillDetectsTranslationContentChanges(t *testing.T) {
	t.Parallel()

	a := `Translated in en: Hello
<br><br>
<p>Hi</p>`
	b := `Translated in en: Hello
<br><br>
<p>Different body</p>`

	na, err := NormalizeByExt(".html", []byte(a))
	require.NoError(t, err)
	nb, err := NormalizeByExt(".html", []byte(b))
	require.NoError(t, err)
	assert.NotEqual(t, string(na), string(nb))
}

func TestNormalizeJS_SearchIndexKeepsSemanticFields(t *testing.T) {
	t.Parallel()

	a := `window.searchIndex = window.searchIndex || {};
window.searchIndex["en"] = [{"title":"A","permalink":"https://example.com/a/"},{"title":"B","permalink":"https://example.com/b/"}];`
	b := `window.searchIndex = window.searchIndex || {};
window.searchIndex["en"] = [{"title":"B","permalink":"https://example.com/b/"},{"title":"A","permalink":"https://example.com/a/"}];`
	c := `window.searchIndex = window.searchIndex || {};
window.searchIndex["en"] = [{"title":"Changed","permalink":"https://example.com/a/"},{"title":"B","permalink":"https://example.com/b/"}];`

	na, err := NormalizeByExt(".js", []byte(a))
	require.NoError(t, err)
	nb, err := NormalizeByExt(".js", []byte(b))
	require.NoError(t, err)
	nc, err := NormalizeByExt(".js", []byte(c))
	require.NoError(t, err)

	assert.Equal(t, string(na), string(nb))
	assert.NotEqual(t, string(na), string(nc))
}

func TestNormalizeJS_LunrIndexKeepsDocumentIDsAndTitles(t *testing.T) {
	t.Parallel()

	a := `window.searchIndex = {"documentStore":{"docs":{"https://example.com/a/":{"title":"A"},"https://example.com/b/":{"title":"B"}}}};`
	b := `window.searchIndex = {"documentStore":{"docs":{"https://example.com/b/":{"title":"B"},"https://example.com/a/":{"title":"A"}}}};`
	c := `window.searchIndex = {"documentStore":{"docs":{"https://example.com/a/":{"title":"Changed"},"https://example.com/b/":{"title":"B"}}}};`

	na, err := NormalizeByExt(".js", []byte(a))
	require.NoError(t, err)
	nb, err := NormalizeByExt(".js", []byte(b))
	require.NoError(t, err)
	nc, err := NormalizeByExt(".js", []byte(c))
	require.NoError(t, err)

	assert.Equal(t, string(na), string(nb))
	assert.NotEqual(t, string(na), string(nc))
}
