package filesystem

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type FileSystem interface {
	fs.FS
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm fs.FileMode) error
	MkdirAll(path string, perm fs.FileMode) error
	Stat(name string) (fs.FileInfo, error)
	RemoveAll(path string) error
}

type DiskFS struct {
	Root string
}

func NewDiskFS(root string) *DiskFS {
	return &DiskFS{Root: root}
}

// ValidatePath requires slash-separated io/fs paths. Reject native separators
// and drive prefixes too, so a path has the same meaning on every platform.
func ValidatePath(name string) error {
	if !fs.ValidPath(name) || strings.Contains(name, "\\") || strings.ContainsRune(name, 0) || (len(name) >= 2 && name[1] == ':') {
		return &fs.PathError{Op: "validate", Path: name, Err: fs.ErrInvalid}
	}
	return nil
}

func (f *DiskFS) openRoot(create bool) (*os.Root, error) {
	if create {
		if err := os.MkdirAll(f.Root, 0o755); err != nil {
			return nil, err
		}
	}
	return os.OpenRoot(f.Root)
}

func (f *DiskFS) Open(name string) (fs.File, error) {
	if err := ValidatePath(name); err != nil {
		return nil, err
	}
	root, err := f.openRoot(false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Open(filepath.FromSlash(name))
}

func (f *DiskFS) ReadFile(name string) ([]byte, error) {
	if err := ValidatePath(name); err != nil {
		return nil, err
	}
	root, err := f.openRoot(false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(filepath.FromSlash(name))
}

func (f *DiskFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	if err := ValidatePath(name); err != nil {
		return err
	}
	root, err := f.openRoot(true)
	if err != nil {
		return err
	}
	defer root.Close()
	local := filepath.FromSlash(name)
	if err := root.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return err
	}
	return root.WriteFile(local, data, perm)
}

func (f *DiskFS) MkdirAll(name string, perm fs.FileMode) error {
	if err := ValidatePath(name); err != nil {
		return err
	}
	root, err := f.openRoot(true)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.MkdirAll(filepath.FromSlash(name), perm)
}

func (f *DiskFS) Stat(name string) (fs.FileInfo, error) {
	if err := ValidatePath(name); err != nil {
		return nil, err
	}
	root, err := f.openRoot(false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.Stat(filepath.FromSlash(name))
}

func (f *DiskFS) RemoveAll(name string) error {
	if err := ValidatePath(name); err != nil {
		return err
	}
	root, err := f.openRoot(false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	return root.RemoveAll(filepath.FromSlash(name))
}
