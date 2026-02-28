package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
	require.NoError(t, os.WriteFile(filepath.Join(root, "zola.toml"), []byte(`
base_url = "http://127.0.0.1:1111"
title = "My Site"
output_dir = "public"
compile_sass = false

[link_checker]
internal_level = "error"
timeout_seconds = 2
use_cache = false
`), 0o644))

	require.NoError(t, runBuild([]string{"--root", root, "--force"}))
	require.FileExists(t, filepath.Join(root, "public", "404.html"))

	// Make check deterministic without internet by using a local page with no outbound links.
	content := "+++\ntitle='No Links'\n+++\nHello"
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "nolinks.md"), []byte(content), 0o644))
	require.NoError(t, runCheck([]string{"--root", root}))
}

func TestRunCheck_WarnModeDoesNotFail(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	root := filepath.Join(t.TempDir(), "site")
	require.NoError(t, runInit([]string{root}))

	require.NoError(t, os.WriteFile(filepath.Join(root, "zola.toml"), []byte(fmt.Sprintf(`
base_url = "http://127.0.0.1:1111"
title = "My Site"
output_dir = "public"
compile_sass = false

[link_checker]
internal_level = "warn"
timeout_seconds = 2
use_cache = false
`)), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "link.md"), []byte("+++\ntitle='Link'\n+++\n[broken]("+ts.URL+")"), 0o644))

	require.NoError(t, runCheck([]string{"--root", root}))
}
