package assets

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src string, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

func CompileSass(basePath string, outputPath string) error {
	sassDir := filepath.Join(basePath, "sass")
	if _, err := os.Stat(sassDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	entries, err := os.ReadDir(sassDir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".scss" && ext != ".sass" {
			continue
		}
		if strings.HasPrefix(name, "_") {
			continue
		}
		src := filepath.Join(sassDir, name)
		dst := filepath.Join(outputPath, "static", strings.TrimSuffix(name, ext)+".css")
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}

		if _, err := exec.LookPath("sass"); err == nil {
			cmd := exec.Command("sass", "--no-source-map", src, dst)
			if output, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
				return fmt.Errorf("sass compile %s: %w: %s", name, cmdErr, strings.TrimSpace(string(output)))
			}
		} else {
			// Fallback: copy raw file into css output so builds stay usable.
			if err := copyFile(src, dst); err != nil {
				return err
			}
		}
	}

	return nil
}
