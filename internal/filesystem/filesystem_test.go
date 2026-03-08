package filesystem

import (
	"io/fs"
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
