package gitdates

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseLog(t *testing.T) {
	t.Parallel()

	at := func(s string) time.Time {
		ts, err := time.Parse(time.RFC3339, s)
		require.NoError(t, err)
		return ts
	}

	for _, tc := range []struct {
		name    string
		log     string
		ignored map[string]bool
		want    map[string]time.Time
	}{
		{
			name: "newest commit wins",
			log: "\x01aaa 2024-03-01T10:00:00+01:00\n\ncontent/a.md\n\n" +
				"\x01bbb 2024-01-01T10:00:00+01:00\n\ncontent/a.md\ncontent/b.md\n",
			want: map[string]time.Time{
				"content/a.md": at("2024-03-01T10:00:00+01:00"),
				"content/b.md": at("2024-01-01T10:00:00+01:00"),
			},
		},
		{
			name: "ignored commits are skipped",
			log: "\x01aaa 2024-03-01T10:00:00+01:00\n\ncontent/a.md\n\n" +
				"\x01bbb 2024-01-01T10:00:00+01:00\n\ncontent/a.md\ncontent/b.md\n",
			ignored: map[string]bool{"aaa": true},
			want: map[string]time.Time{
				"content/a.md": at("2024-01-01T10:00:00+01:00"),
				"content/b.md": at("2024-01-01T10:00:00+01:00"),
			},
		},
		{
			name:    "only ignored commits leave no date",
			log:     "\x01aaa 2024-03-01T10:00:00+01:00\n\ncontent/a.md\n",
			ignored: map[string]bool{"aaa": true},
			want:    map[string]time.Time{},
		},
		{
			name: "empty log",
			log:  "",
			want: map[string]time.Time{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseLog([]byte(tc.log), tc.ignored)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParseLogBadTime(t *testing.T) {
	t.Parallel()
	_, err := parseLog([]byte("\x01aaa nonsense\n\ncontent/a.md\n"), nil)
	require.Error(t, err)
}

func TestReadIgnoredRevs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git-blame-ignore-revs"),
		[]byte("# migration\nabc123 # inline\n\n  def456  \n"), 0o644))

	require.Equal(t, map[string]bool{"abc123": true, "def456": true}, readIgnoredRevs(dir))
	require.Nil(t, readIgnoredRevs(t.TempDir()))
}

func git(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), append([]string{
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	}, env...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func TestLastCommits(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Parallel()

	dir := t.TempDir()
	write := func(name, body string) {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	commit := func(date, msg string) string {
		git(t, dir, nil, "add", "-A")
		git(t, dir, []string{"GIT_COMMITTER_DATE=" + date, "GIT_AUTHOR_DATE=" + date}, "commit", "-m", msg)
		return git(t, dir, nil, "rev-parse", "HEAD")
	}

	git(t, dir, nil, "init", "-q")
	write("content/a.md", "1")
	write("content/ünï/index.md", "1")
	write("other.txt", "1")
	commit("2024-01-01T10:00:00Z", "first")

	write("content/a.md", "2")
	commit("2024-02-01T10:00:00Z", "edit a")

	write("content/ünï/index.md", "reformatted")
	reformat := commit("2024-03-01T10:00:00Z", "reformat")

	got, err := LastCommits(dir, "content")
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"content/a.md":         "2024-02-01T10:00:00Z",
		"content/ünï/index.md": "2024-03-01T10:00:00Z",
	}, formatAll(got))

	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git-blame-ignore-revs"), []byte(reformat+"\n"), 0o644))
	got, err = LastCommits(dir, "content")
	require.NoError(t, err)
	require.Equal(t, "2024-01-01T10:00:00Z", got["content/ünï/index.md"].UTC().Format(time.RFC3339))
}

func TestLastCommitsNoRepo(t *testing.T) {
	t.Parallel()
	got, err := LastCommits(t.TempDir(), "content")
	require.NoError(t, err)
	require.Empty(t, got)
}

func formatAll(in map[string]time.Time) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v.UTC().Format(time.RFC3339)
	}
	return out
}
