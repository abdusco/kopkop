package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunInitBuildCheck(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "site")
	require.NoError(t, runInit([]string{root}))
	require.FileExists(t, filepath.Join(root, "zola.toml"))

	require.NoError(t, runBuild([]string{"--root", root, "--force"}))
	require.FileExists(t, filepath.Join(root, "public", "404.html"))

	// Make check deterministic without internet by using a local page with no outbound links.
	content := "+++\ntitle='No Links'\n+++\nHello"
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "nolinks.md"), []byte(content), 0o644))
	require.NoError(t, runCheck([]string{"--root", root}))
}
