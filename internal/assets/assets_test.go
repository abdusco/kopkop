package assets

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/abdusco/kopkop/internal/filesystem"

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

func TestCopyDirectoryRejectsSymlinkEscapes(t *testing.T) {
	t.Parallel()
	for _, side := range []string{"source file", "source directory", "destination directory", "destination file"} {
		t.Run(side, func(t *testing.T) {
			t.Parallel()
			parent := t.TempDir()
			src := filepath.Join(parent, "src")
			dst := filepath.Join(parent, "dst")
			outside := filepath.Join(parent, "outside")
			require.NoError(t, os.MkdirAll(src, 0o755))
			require.NoError(t, os.MkdirAll(dst, 0o755))
			require.NoError(t, os.MkdirAll(outside, 0o755))
			sentinel := filepath.Join(outside, "sentinel")
			require.NoError(t, os.WriteFile(sentinel, []byte("keep me"), 0o644))
			switch side {
			case "source file":
				require.NoError(t, os.Symlink("../outside/sentinel", filepath.Join(src, "sentinel")))
				require.Error(t, CopyDirectory(src, dst))
				require.NoFileExists(t, filepath.Join(dst, "sentinel"))
			case "source directory":
				require.NoError(t, os.Symlink("../outside", filepath.Join(src, "static")))
				require.Error(t, CopyDirectoryFS(filesystem.NewDiskFS(src), "static", filesystem.NewDiskFS(dst)))
			case "destination directory":
				require.NoError(t, os.MkdirAll(filepath.Join(src, "nested"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(src, "nested", "sentinel"), []byte("overwrite"), 0o644))
				require.NoError(t, os.Symlink("../outside", filepath.Join(dst, "nested")))
				require.Error(t, CopyDirectory(src, dst))
			case "destination file":
				require.NoError(t, os.WriteFile(filepath.Join(src, "sentinel"), []byte("overwrite"), 0o644))
				require.NoError(t, os.Symlink("../outside/sentinel", filepath.Join(dst, "sentinel")))
				require.Error(t, CopyDirectory(src, dst))
			}
			data, err := os.ReadFile(sentinel)
			require.NoError(t, err)
			require.Equal(t, "keep me", string(data))
		})
	}
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
