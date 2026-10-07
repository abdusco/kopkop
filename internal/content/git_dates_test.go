package content

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/abdusco/kopkop/internal/config"
)

func TestLoadLibrary_GitDates(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Parallel()

	root := t.TempDir()
	for name, body := range map[string]string{
		"content/_index.md":           "+++\ntitle='Home'\n+++\n",
		"content/plain.md":            "+++\ntitle='Plain'\n+++\n",
		"content/explicit.md":         "+++\ntitle='Explicit'\nupdated=2020-05-06\n+++\n",
		"content/bundle/index.md":     "+++\ntitle='Bundle'\n+++\n",
		"content/uncommitted/page.md": "",
	} {
		if body == "" {
			continue
		}
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
	}
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_AUTHOR_DATE=2024-02-03T04:05:06Z", "GIT_COMMITTER_DATE=2024-02-03T04:05:06Z")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	run("init", "-q")
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	require.NoError(t, os.WriteFile(filepath.Join(root, "content", "new.md"), []byte("+++\ntitle='New'\n+++\n"), 0o644))

	committed := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)

	for _, tc := range []struct {
		name    string
		gitDate bool
		page    string
		want    *time.Time
	}{
		{name: "disabled leaves updated empty", gitDate: false, page: "plain.md", want: nil},
		{name: "enabled uses last commit", gitDate: true, page: "plain.md", want: &committed},
		{name: "bundles use index.md", gitDate: true, page: "bundle/index.md", want: &committed},
		{name: "front matter wins", gitDate: true, page: "explicit.md", want: ptr(time.Date(2020, 5, 6, 0, 0, 0, 0, time.UTC))},
		{name: "uncommitted files stay empty", gitDate: true, page: "new.md", want: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.BaseURL = "https://example.com"
			cfg.GitDates = tc.gitDate

			lib, err := LoadLibrary(root, cfg, LoadOptions{})
			require.NoError(t, err)
			page := lib.Pages[tc.page]
			require.NotNil(t, page)
			if tc.want == nil {
				require.Nil(t, page.Updated)
				return
			}
			require.NotNil(t, page.Updated)
			require.True(t, tc.want.Equal(*page.Updated), "got %s", page.Updated)
		})
	}
}

func ptr[T any](v T) *T { return &v }
