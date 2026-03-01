package assets

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCleanOutput_RemovesExistingDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	out := filepath.Join(root, "public")
	require.NoError(t, os.MkdirAll(filepath.Join(out, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(out, "nested", "old.txt"), []byte("stale"), 0o644))

	require.NoError(t, CleanOutput(out))
	require.DirExists(t, out)
	require.NoFileExists(t, filepath.Join(out, "nested", "old.txt"))
}

func TestCopyDirectory_CopiesNestedFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	require.NoError(t, os.MkdirAll(filepath.Join(src, "a", "b"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "a", "b", "file.txt"), []byte("hello"), 0o644))

	require.NoError(t, CopyDirectory(src, dst))

	b, err := os.ReadFile(filepath.Join(dst, "a", "b", "file.txt"))
	require.NoError(t, err)
	require.Equal(t, "hello", string(b))
}

func TestCopyDirectory_MissingSourceIsNoop(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src := filepath.Join(root, "does-not-exist")
	dst := filepath.Join(root, "dst")

	require.NoError(t, CopyDirectory(src, dst))
	require.NoDirExists(t, dst)
}
