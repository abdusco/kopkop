package filesystem

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryFS_ReadWriteAndRemoveAll(t *testing.T) {
	t.Parallel()

	m := NewMemoryFS()
	require.NoError(t, m.MkdirAll("a/b", 0o755))
	require.NoError(t, m.WriteFile("a/b/hello.txt", []byte("hello"), 0o644))

	b, err := m.ReadFile("a/b/hello.txt")
	require.NoError(t, err)
	require.Equal(t, "hello", string(b))

	st, err := m.Stat("a/b/hello.txt")
	require.NoError(t, err)
	require.False(t, st.IsDir())

	require.NoError(t, m.RemoveAll("a"))
	_, err = m.ReadFile("a/b/hello.txt")
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestFileSystemsRejectInvalidPaths(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"..", "../outside", "a/../../outside", "/outside", "//outside", "a/../outside", `..\outside`, `C:\outside`, "C:/outside", "a//b", "", "bad\x00name"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, fys := range []FileSystem{NewDiskFS(filepath.Join(t.TempDir(), "missing")), NewMemoryFS()} {
				_, err := fys.Open(name)
				require.ErrorIs(t, err, fs.ErrInvalid)
				_, err = fys.ReadFile(name)
				require.ErrorIs(t, err, fs.ErrInvalid)
				_, err = fys.Stat(name)
				require.ErrorIs(t, err, fs.ErrInvalid)
				require.ErrorIs(t, fys.WriteFile(name, []byte("overwrite"), 0o644), fs.ErrInvalid)
				require.ErrorIs(t, fys.MkdirAll(name, 0o755), fs.ErrInvalid)
				require.ErrorIs(t, fys.RemoveAll(name), fs.ErrInvalid)
			}
		})
	}
}

func TestDiskFSRejectsSymlinkEscapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		linkTarget string
		rel        string
	}{
		{name: "relative directory", linkTarget: "../outside", rel: "link/sentinel"},
		{name: "relative file", linkTarget: "../outside/sentinel", rel: "link"},
		{name: "absolute directory", linkTarget: "absolute", rel: "link/sentinel"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parent := t.TempDir()
			root := filepath.Join(parent, "root")
			outside := filepath.Join(parent, "outside")
			require.NoError(t, os.MkdirAll(root, 0o755))
			require.NoError(t, os.MkdirAll(outside, 0o755))
			sentinel := filepath.Join(outside, "sentinel")
			require.NoError(t, os.WriteFile(sentinel, []byte("keep me"), 0o644))
			target := tc.linkTarget
			if target == "absolute" {
				target = outside
			}
			require.NoError(t, os.Symlink(target, filepath.Join(root, "link")))
			fys := NewDiskFS(root)
			_, err := fys.Open(tc.rel)
			require.Error(t, err)
			_, err = fys.ReadFile(tc.rel)
			require.Error(t, err)
			_, err = fys.Stat(tc.rel)
			require.Error(t, err)
			require.Error(t, fys.WriteFile(tc.rel, []byte("overwrite"), 0o644))
			require.Error(t, fys.MkdirAll("link/new-dir", 0o755))
			// Removing the link itself is safe; removing a child must not follow it.
			if tc.rel != "link" {
				require.Error(t, fys.RemoveAll(tc.rel))
			}
			data, err := os.ReadFile(sentinel)
			require.NoError(t, err)
			require.Equal(t, "keep me", string(data))
			require.NoDirExists(t, filepath.Join(outside, "new-dir"))
			require.NoError(t, fys.RemoveAll("link"))
			require.FileExists(t, sentinel)
		})
	}
}

func TestDiskFSAllowsContainedSymlinks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fys := NewDiskFS(root)
	require.NoError(t, fys.WriteFile("real/file", []byte("original"), 0o644))
	require.NoError(t, os.Symlink("real", filepath.Join(root, "link")))
	require.NoError(t, fys.WriteFile("link/file", []byte("updated"), 0o644))
	data, err := fys.ReadFile("link/file")
	require.NoError(t, err)
	require.Equal(t, "updated", string(data))
	require.NoError(t, fys.MkdirAll("link/nested", 0o755))
	require.DirExists(t, filepath.Join(root, "real", "nested"))
}

func TestDiskFS_ReadWrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	d := NewDiskFS(root)
	require.NoError(t, d.WriteFile("x/y/z.txt", []byte("ok"), 0o644))

	b, err := d.ReadFile("x/y/z.txt")
	require.NoError(t, err)
	require.Equal(t, "ok", string(b))

	require.FileExists(t, filepath.Join(root, "x", "y", "z.txt"))
}
