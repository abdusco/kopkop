package filesystem

import (
	"fmt"
	"io"
	"io/fs"
	"syscall"
	"testing"
	"testing/fstest"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMemoryFSContracts(t *testing.T) {
	m := NewMemoryFS()
	require.NoError(t, m.MkdirAll("empty", 0o750))
	require.NoError(t, m.WriteFile("a/b/file.txt", []byte("hello"), 0o640))
	require.NoError(t, m.WriteFile("root.txt", []byte("root"), 0o600))
	require.NoError(t, fstest.TestFS(m, "a/b/file.txt", "root.txt"))
	// Force generic io/fs code to use Open and directory handles.
	require.NoError(t, fstest.TestFS(struct{ fs.FS }{m}, "a/b/file.txt", "root.txt"))
}

func TestMemoryFSRemoveAll(t *testing.T) {
	for _, tc := range []struct {
		name      string
		remove    string
		remaining []string
	}{
		{"root", ".", nil},
		{"subtree", "a", []string{"ab/file", "root.txt"}},
		{"file", "a/file", []string{"a/nested/file", "ab/file", "root.txt"}},
		{"missing", "missing", []string{"a/file", "a/nested/file", "ab/file", "root.txt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMemoryFS()
			for _, name := range []string{"a/file", "a/nested/file", "ab/file", "root.txt"} {
				require.NoError(t, m.WriteFile(name, []byte(name), 0o644))
			}
			require.NoError(t, m.RemoveAll(tc.remove))
			require.NoError(t, m.RemoveAll(tc.remove))
			remaining := []string{}
			require.NoError(t, fs.WalkDir(m, ".", func(name string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					remaining = append(remaining, name)
				}
				return nil
			}))
			require.ElementsMatch(t, tc.remaining, remaining)
			info, err := m.Stat(".")
			require.NoError(t, err)
			require.True(t, info.IsDir())
			if tc.remove == "." {
				entries, err := fs.ReadDir(m, ".")
				require.NoError(t, err)
				require.Empty(t, entries)
				require.NoError(t, m.WriteFile("new/file", []byte("new"), 0o644))
			}
		})
	}
}

func TestMemoryFSRejectsFileDirectoryConflicts(t *testing.T) {
	for _, tc := range []struct {
		name string
		op   string
		path string
		err  error
	}{
		{"write directory", "write", "dir", syscall.EISDIR},
		{"write root", "write", ".", syscall.EISDIR},
		{"write through file", "write", "file/nested/new", syscall.ENOTDIR},
		{"mkdir file", "mkdir", "file", syscall.ENOTDIR},
		{"mkdir through file", "mkdir", "file/nested/new", syscall.ENOTDIR},
		{"read directory", "read", "dir", syscall.EISDIR},
		{"readdir file", "readdir", "file", syscall.ENOTDIR},
		{"open through file", "open", "file/nested", syscall.ENOTDIR},
		{"stat through file", "stat", "file/nested", syscall.ENOTDIR},
		{"remove through file", "remove", "file/nested", syscall.ENOTDIR},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMemoryFS()
			require.NoError(t, m.MkdirAll("dir", 0o755))
			require.NoError(t, m.WriteFile("file", []byte("keep"), 0o640))
			var err error
			switch tc.op {
			case "write":
				err = m.WriteFile(tc.path, []byte("overwrite"), 0o644)
			case "mkdir":
				err = m.MkdirAll(tc.path, 0o755)
			case "read":
				_, err = m.ReadFile(tc.path)
			case "readdir":
				_, err = m.ReadDir(tc.path)
			case "open":
				_, err = m.Open(tc.path)
			case "stat":
				_, err = m.Stat(tc.path)
			case "remove":
				err = m.RemoveAll(tc.path)
			}
			require.ErrorIs(t, err, tc.err)
			var pathErr *fs.PathError
			require.ErrorAs(t, err, &pathErr)
			require.Equal(t, tc.path, pathErr.Path)
			body, err := m.ReadFile("file")
			require.NoError(t, err)
			require.Equal(t, "keep", string(body))
			entries, err := m.ReadDir(".")
			require.NoError(t, err)
			require.Len(t, entries, 2)
		})
	}
}

func TestMemoryFSDirectoryHandleReads(t *testing.T) {
	for _, count := range []int{-1, 0, 1, 2, 10} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			m := NewMemoryFS()
			for _, name := range []string{"z", "a", "middle"} {
				require.NoError(t, m.WriteFile(name, []byte(name), 0o644))
			}
			handle, err := m.Open(".")
			require.NoError(t, err)
			defer handle.Close()
			dir, ok := handle.(fs.ReadDirFile)
			require.True(t, ok)
			names := []string{}
			for {
				entries, err := dir.ReadDir(count)
				for _, entry := range entries {
					names = append(names, entry.Name())
				}
				if count <= 0 {
					require.NoError(t, err)
					break
				}
				if err == io.EOF {
					require.Empty(t, entries)
					break
				}
				require.NoError(t, err)
				require.NotEmpty(t, entries)
				require.LessOrEqual(t, len(entries), count)
			}
			require.Equal(t, []string{"a", "middle", "z"}, names)
			entries, err := dir.ReadDir(1)
			require.ErrorIs(t, err, io.EOF)
			require.Empty(t, entries)
			entries, err = dir.ReadDir(0)
			require.NoError(t, err)
			require.NotNil(t, entries)
			require.Empty(t, entries)
		})
	}
}

func TestMemoryFSModTimeTracksWrites(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := NewMemoryFS()
		created := time.Now()
		require.NoError(t, m.WriteFile("a.txt", []byte("one"), 0o644))
		info, err := m.Stat("a.txt")
		require.NoError(t, err)
		require.Equal(t, created, info.ModTime())

		time.Sleep(time.Hour) // advances the bubble's clock instantly
		require.NoError(t, m.WriteFile("a.txt", []byte("two"), 0o644))
		info, err = m.Stat("a.txt")
		require.NoError(t, err)
		require.Equal(t, created.Add(time.Hour), info.ModTime())
	})
}

func TestMemoryFSMetadataAndSnapshots(t *testing.T) {
	m := NewMemoryFS()
	before := time.Now()
	require.NoError(t, m.MkdirAll("dir/nested", 0o710))
	input := []byte("original")
	require.NoError(t, m.WriteFile("dir/file", input, 0o640))
	input[0] = 'X'
	info, err := m.Stat("dir/file")
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0o640), info.Mode())
	require.False(t, info.ModTime().Before(before))
	require.False(t, info.ModTime().After(time.Now()))
	file, err := m.Open("dir/file")
	require.NoError(t, err)
	defer file.Close()
	dir, err := m.Open("dir")
	require.NoError(t, err)
	defer dir.Close()
	fileInfo, err := file.Stat()
	require.NoError(t, err)
	require.Equal(t, info, fileInfo)
	require.NoError(t, m.MkdirAll("dir/nested", 0o777))
	dirInfo, err := m.Stat("dir/nested")
	require.NoError(t, err)
	require.Equal(t, fs.ModeDir|0o710, dirInfo.Mode())
	require.NoError(t, m.WriteFile("dir/file", []byte("new"), 0o777))
	updated, err := m.Stat("dir/file")
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0o640), updated.Mode())
	require.False(t, updated.ModTime().Before(info.ModTime()))
	body, err := m.ReadFile("dir/file")
	require.NoError(t, err)
	body[0] = 'X'
	body, err = m.ReadFile("dir/file")
	require.NoError(t, err)
	require.Equal(t, "new", string(body))
	require.NoError(t, m.RemoveAll("."))
	body, err = io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, "original", string(body))
	entries, err := dir.(fs.ReadDirFile).ReadDir(-1)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	snapshotInfo, err := entries[0].Info()
	require.NoError(t, err)
	require.Equal(t, info, snapshotInfo)
}

func TestMemoryFSClosedHandles(t *testing.T) {
	for _, name := range []string{".", "file"} {
		t.Run(name, func(t *testing.T) {
			m := NewMemoryFS()
			require.NoError(t, m.WriteFile("file", []byte("data"), 0o644))
			handle, err := m.Open(name)
			require.NoError(t, err)
			require.NoError(t, handle.Close())
			_, err = handle.Read(make([]byte, 1))
			require.ErrorIs(t, err, fs.ErrClosed)
			_, err = handle.Stat()
			require.ErrorIs(t, err, fs.ErrClosed)
			require.ErrorIs(t, handle.Close(), fs.ErrClosed)
			if dir, ok := handle.(fs.ReadDirFile); ok {
				_, err := dir.ReadDir(1)
				require.ErrorIs(t, err, fs.ErrClosed)
			}
		})
	}
}

func TestMemoryFSConcurrentAccess(t *testing.T) {
	m := NewMemoryFS()
	for i := 0; i < 8; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			dir := fmt.Sprintf("worker%d", i)
			for j := 0; j < 20; j++ {
				require.NoError(t, m.WriteFile(dir+"/file", []byte("data"), 0o644))
				body, err := m.ReadFile(dir + "/file")
				require.NoError(t, err)
				require.Equal(t, "data", string(body))
				_, err = fs.ReadDir(m, ".")
				require.NoError(t, err)
				require.NoError(t, m.RemoveAll(dir))
			}
		})
	}
}
