package harness

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/abdusco/kopkop/internal/site"
)

func BuildWithKopkop(root string, configPath string, outputDir string, includeDrafts bool) error {
	s, err := site.New(root, configPath)
	if err != nil {
		return err
	}
	if err := s.Load(includeDrafts); err != nil {
		return err
	}
	return s.Build(site.BuildOptions{
		IncludeDrafts: includeDrafts,
		OutputDir:     outputDir,
		BuildMode:     site.BuildDisk,
		Force:         true,
	})
}

func BuildWithZola(zolaBin string, root string, configName string, outputDir string, drafts bool) error {
	if zolaBin == "" {
		return fmt.Errorf("zola binary path is empty")
	}
	if _, err := os.Stat(zolaBin); err != nil {
		return fmt.Errorf("zola binary not found: %w", err)
	}

	args := []string{"build", "--root", root, "--config", configName, "--output-dir", outputDir, "--force"}
	if drafts {
		args = append(args, "--drafts")
	}
	cmd := exec.Command(zolaBin, args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("zola build failed: %w: %s", err, string(out))
	}
	return nil
}

func FixturePath(parts ...string) string {
	all := append([]string{"tests", "fixtures"}, parts...)
	return filepath.Join(all...)
}
