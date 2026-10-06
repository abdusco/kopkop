package filesystem

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
)

type failingOutputFS struct {
	FileSystem
	err error
}

func (f failingOutputFS) WriteFile(string, []byte, fs.FileMode) error { return f.err }
func (f failingOutputFS) MkdirAll(string, fs.FileMode) error          { return f.err }
func (f failingOutputFS) RemoveAll(string) error                      { return f.err }

func TestMirrorFSMutationErrors(t *testing.T) {
	for _, target := range []string{"primary", "mirror"} {
		for _, operation := range []string{"write", "mkdir", "remove"} {
			t.Run(target+"/"+operation, func(t *testing.T) {
				failure := errors.New("output unavailable")
				primary, mirror := NewMemoryFS(), NewMemoryFS()
				m := &MirrorFS{Primary: primary, Mirror: mirror}
				if target == "primary" {
					m.Primary = failingOutputFS{FileSystem: primary, err: failure}
				} else {
					m.Mirror = failingOutputFS{FileSystem: mirror, err: failure}
				}
				var err error
				switch operation {
				case "write":
					err = m.WriteFile("file", []byte("data"), 0o644)
				case "mkdir":
					err = m.MkdirAll("dir", 0o755)
				case "remove":
					err = m.RemoveAll("file")
				}
				require.ErrorIs(t, err, failure)
				if target == "primary" {
					entries, err := mirror.ReadDir(".")
					require.NoError(t, err)
					require.Empty(t, entries)
				}
			})
		}
	}
}

func TestMirrorFSReadsPrimaryAndMirrorsMutations(t *testing.T) {
	primary, mirror := NewMemoryFS(), NewMemoryFS()
	m := &MirrorFS{Primary: primary, Mirror: mirror}
	require.NoError(t, m.MkdirAll("dir", 0o755))
	require.NoError(t, m.WriteFile("dir/file", []byte("primary"), 0o644))
	body, err := mirror.ReadFile("dir/file")
	require.NoError(t, err)
	require.Equal(t, "primary", string(body))
	require.NoError(t, mirror.WriteFile("dir/file", []byte("mirror"), 0o644))
	body, err = fs.ReadFile(m, "dir/file")
	require.NoError(t, err)
	require.Equal(t, "primary", string(body))
	file, err := m.Open("dir/file")
	require.NoError(t, err)
	defer file.Close()
	info, err := m.Stat("dir/file")
	require.NoError(t, err)
	require.Equal(t, int64(len("primary")), info.Size())
	require.NoError(t, m.RemoveAll("dir"))
	for _, output := range []FileSystem{primary, mirror} {
		_, err := output.Stat("dir")
		require.ErrorIs(t, err, fs.ErrNotExist)
	}
}
