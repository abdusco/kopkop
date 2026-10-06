package assets

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/abdusco/kopkop/internal/filesystem"
)

func CleanOutput(path string) error {
	if _, err := os.Stat(path); err == nil {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return os.MkdirAll(path, 0o755)
}

func CopyDirectory(src string, dst string) error {
	return CopyDirectoryFS(filesystem.NewDiskFS(src), ".", filesystem.NewDiskFS(dst))
}

func CopyDirectoryFS(sourceFS filesystem.FileSystem, src string, outputFS filesystem.FileSystem) error {
	if _, err := sourceFS.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return fs.WalkDir(sourceFS, src, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.FromSlash(src), filepath.FromSlash(name))
		if err != nil {
			return err
		}
		if d.IsDir() {
			return outputFS.MkdirAll(filepath.ToSlash(rel), 0o755)
		}
		data, err := sourceFS.ReadFile(name)
		if err != nil {
			return err
		}
		return outputFS.WriteFile(filepath.ToSlash(rel), data, 0o644)
	})
}
