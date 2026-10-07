package main

import (
	"flag"
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

[link_checker]
internal_level = "error"
timeout_seconds = 2
use_cache = false
`), 0o644))

	require.NoError(t, runBuild([]string{"--root", root}))
	require.FileExists(t, filepath.Join(root, "public", "404.html"))
	// A repeated build replaces the previous output.
	require.NoError(t, runBuild([]string{"--root", root}))

	// Make check deterministic without internet by using a local page with no outbound links.
	content := "+++\ntitle='No Links'\n+++\nHello"
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "nolinks.md"), []byte(content), 0o644))
	require.NoError(t, runCheck([]string{"--root", root}))
}

func TestHelpFlagIsNotAFailure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		run  func([]string) error
	}{
		{"init", runInit},
		{"build", runBuild},
		{"serve", runServe},
		{"check", runCheck},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.run([]string{"-h"})
			require.ErrorIs(t, err, flag.ErrHelp)
			// exitOnError would call log.Fatalf (exiting the test binary) on a real error.
			exitOnError(tc.name, err)
		})
	}
}

func TestRunInitDoesNotScaffoldKeepFile(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "site")
	require.NoError(t, runInit([]string{root}))
	require.NoFileExists(t, filepath.Join(root, "static", ".keep"))
	require.FileExists(t, filepath.Join(root, "static", "style.css"))
}

func TestRunCheckOutputDirAndBaseURL(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "site")
	require.NoError(t, runInit([]string{root}))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "nolinks.md"), []byte("+++\ntitle='No Links'\n+++\nHello"), 0o644))
	out := filepath.Join(t.TempDir(), "out")

	require.NoError(t, runCheck([]string{"--root", root, "--output-dir", out, "--base-url", "https://example.com"}))
	require.FileExists(t, filepath.Join(out, "404.html"))
	require.NoDirExists(t, filepath.Join(root, "public"))
}

func TestVersion(t *testing.T) {
	t.Parallel()
	require.NotEmpty(t, version())
}

func TestRunInitConflicts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path       string
		directory, force bool
	}{
		{name: "existing late template", path: "templates/index.html"},
		{name: "existing late static file", path: "static/style.css"},
		{name: "directory destination", path: "templates/page.html", directory: true, force: true},
		{name: "file parent", path: "static", force: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			p := filepath.Join(root, tc.path)
			require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
			if tc.directory {
				require.NoError(t, os.Mkdir(p, 0o755))
			} else {
				require.NoError(t, os.WriteFile(p, []byte("keep me"), 0o644))
			}
			args := []string{root}
			if tc.force {
				args = append(args, "--force")
			}
			require.Error(t, runInit(args))
			require.NoFileExists(t, filepath.Join(root, "zola.toml"))
			if !tc.directory {
				data, err := os.ReadFile(p)
				require.NoError(t, err)
				require.Equal(t, "keep me", string(data))
			}
		})
	}
}

func TestRunInitForceFlagPlacement(t *testing.T) {
	t.Parallel()
	for _, after := range []bool{false, true} {
		t.Run(fmt.Sprint(after), func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "zola.toml"), []byte("replace me"), 0o644))
			args := []string{"--force", root}
			if after {
				args = []string{root, "--force"}
			}
			require.NoError(t, runInit(args))
			require.FileExists(t, filepath.Join(root, "static", "style.css"))
		})
	}
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

[link_checker]
internal_level = "warn"
external_level = "warn"
timeout_seconds = 2
use_cache = false
`)), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "link.md"), []byte("+++\ntitle='Link'\n+++\n[broken]("+ts.URL+")"), 0o644))

	require.NoError(t, runCheck([]string{"--root", root}))
}

func TestRunCheckSeverityPolicies(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer ts.Close()
	for _, tc := range []struct {
		name, internal, external, html, markdown string
		wantErr                                  bool
	}{
		{"external error independent", "warn", "error", `<a href="` + ts.URL + `">bad</a>`, "", true},
		{"external warning independent", "error", "warn", `<a href="` + ts.URL + `">bad</a>`, "", false},
		{"internal error independent", "error", "warn", `<a href='/missing/'>bad</a>`, "", true},
		{"internal warning independent", "warn", "error", `<a href='/missing/'>bad</a>`, "", false},
		{"internal anchor error", "error", "warn", `{{ page.content | safe }}`, "[bad](@/link.md#missing)", true},
		{"missing content warning", "warn", "error", `{{ page.content | safe }}`, "[bad](@/missing.md)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "site")
			require.NoError(t, runInit([]string{root}))
			cfg := fmt.Sprintf("base_url='https://example.com'\n[link_checker]\ninternal_level='%s'\nexternal_level='%s'\nuse_cache=false\n", tc.internal, tc.external)
			require.NoError(t, os.WriteFile(filepath.Join(root, "zola.toml"), []byte(cfg), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(root, "templates/page.html"), []byte(tc.html), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(root, "content/link.md"), []byte(tc.markdown), 0o644))
			err := runCheck([]string{"--root", root})
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
