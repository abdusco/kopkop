package site

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// validateOutputPath runs for all build modes: memory builds also write some
// assets to disk. Resolve existing parents even when the output does not exist.
func validateOutputPath(basePath, configPath, outputPath string, extraWatchPaths []string) error {
	if strings.TrimSpace(outputPath) == "" {
		return fmt.Errorf("output directory must not be empty")
	}
	base, err := canonicalPath(basePath)
	if err != nil {
		return fmt.Errorf("resolve site directory: %w", err)
	}
	output, err := canonicalPath(outputPath)
	if err != nil {
		return fmt.Errorf("resolve output directory: %w", err)
	}
	if pathContains(output, base) {
		return fmt.Errorf("unsafe output directory %q: contains the site directory", outputPath)
	}

	protected := []string{configPath}
	for _, name := range []string{"content", "templates", "static", "themes", "data", ".git"} {
		protected = append(protected, filepath.Join(basePath, name))
	}
	// Any hidden directory (.git, tool state) must survive an output wipe.
	if entries, err := os.ReadDir(basePath); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), ".") {
				protected = append(protected, filepath.Join(basePath, entry.Name()))
			}
		}
	}
	for _, name := range extraWatchPaths {
		if !filepath.IsAbs(name) {
			name = filepath.Join(basePath, name)
		}
		protected = append(protected, name)
	}
	for _, name := range protected {
		if name == "" {
			continue
		}
		source, err := canonicalPath(name)
		if err != nil {
			return fmt.Errorf("resolve source path %q: %w", name, err)
		}
		if pathContains(output, source) || pathContains(source, output) {
			return fmt.Errorf("unsafe output directory %q: overlaps source path %q", outputPath, name)
		}
	}
	return nil
}

func pathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func canonicalPath(name string) (string, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	var missing []string
	for {
		_, err := os.Lstat(abs)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(abs)
			if err != nil {
				return "", err
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		missing = append(missing, filepath.Base(abs))
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", err
		}
		abs = parent
	}
}
