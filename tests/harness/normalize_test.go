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
		tc := tc
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
