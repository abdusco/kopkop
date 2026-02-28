package assets

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileSassDir_FallbackCopiesWhenSassBinaryMissingOrUnavailable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sassDir := filepath.Join(root, "sass")
	out := filepath.Join(root, "public")
	require.NoError(t, os.MkdirAll(sassDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sassDir, "site.scss"), []byte("$x: red; body { color: $x; }"), 0o644))

	require.NoError(t, CompileSassDir(sassDir, out))
	require.FileExists(t, filepath.Join(out, "site.css"))
}

func TestCompileSassDir_CompilesNestedEntries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sassDir := filepath.Join(root, "sass")
	out := filepath.Join(root, "public")
	require.NoError(t, os.MkdirAll(filepath.Join(sassDir, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sassDir, "nested", "site.scss"), []byte("$x: red; body { color: $x; }"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sassDir, "nested", "_partial.scss"), []byte("$x: red;"), 0o644))

	require.NoError(t, CompileSassDir(sassDir, out))
	require.FileExists(t, filepath.Join(out, "nested", "site.css"))
	require.NoFileExists(t, filepath.Join(out, "nested", "_partial.css"))
}
