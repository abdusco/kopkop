package server

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/abdusco/kopkop/internal/assets"
	"github.com/abdusco/kopkop/internal/filesystem"
	"github.com/abdusco/kopkop/internal/site"
)

// storeOutput writes a completed snapshot before replacing the disk directory.
func storeOutput(s *site.Site) error {
	parent := filepath.Dir(s.OutputPath)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".kopkop-serve-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := assets.CopyDirectoryFS(s.MemoryOutput, ".", filesystem.NewDiskFS(stage)); err != nil {
		return fmt.Errorf("stage serve output: %w", err)
	}
	backup := stage + "-previous"
	previous := false
	if _, err := os.Lstat(s.OutputPath); err == nil {
		if err := os.Rename(s.OutputPath, backup); err != nil {
			return fmt.Errorf("move previous serve output: %w", err)
		}
		previous = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(stage, s.OutputPath); err != nil {
		if previous {
			if restoreErr := os.Rename(backup, s.OutputPath); restoreErr != nil {
				return fmt.Errorf("publish serve output: %v; restore previous output from %q: %w", err, backup, restoreErr)
			}
		}
		return fmt.Errorf("publish serve output: %w", err)
	}
	if previous {
		if err := os.RemoveAll(backup); err != nil {
			log.Printf("remove previous serve output %q: %v", backup, err)
		}
	}
	return nil
}
