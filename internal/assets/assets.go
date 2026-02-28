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
	return CompileSassDir(filepath.Join(basePath, "sass"), outputPath)
}

func CompileSassDir(sassDir string, outputPath string) error {
	if _, err := os.Stat(sassDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	_, sassErr := exec.LookPath("sass")

	return filepath.WalkDir(sassDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".scss" && ext != ".sass" {
			return nil
		}
		if strings.HasPrefix(name, "_") {
			return nil
		}

		rel, relErr := filepath.Rel(sassDir, path)
		if relErr != nil {
			return relErr
		}
		dstRel := strings.TrimSuffix(rel, ext) + ".css"
		dst := filepath.Join(outputPath, dstRel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}

		if sassErr == nil {
			cmd := exec.Command("sass", "--no-source-map", path, dst)
			if output, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
				return fmt.Errorf("sass compile %s: %w: %s", rel, cmdErr, strings.TrimSpace(string(output)))
			}
			return nil
		}

		// Fallback: copy raw file into css output so builds stay usable.
		if err := copyFile(path, dst); err != nil {
			return err
		}
		return nil
	})
}
