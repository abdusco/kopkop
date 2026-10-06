package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/abdusco/kopkop/internal/config"
	"github.com/abdusco/kopkop/internal/site"
	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/require"
)

func TestWatchPlanFiltersEvents(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s := &site.Site{BasePath: root, ConfigPath: filepath.Join(root, "zola.toml"), OutputPath: filepath.Join(root, "public"), Config: config.Config{ExtraWatchPaths: []string{"extra"}}}
	plan := newWatchPlan(s, []string{"custom/file.txt"})
	for _, tc := range []struct {
		name string
		op   fsnotify.Op
		want bool
	}{
		{"content/blog/post.md", fsnotify.Write, true},
		{"content/new/deep", fsnotify.Create, true},
		{"templates/macros/post.html", fsnotify.Rename, true},
		{"themes/demo/templates/page.html", fsnotify.Write, true},
		{"static/css/style.css", fsnotify.Remove, true},
		{"data/info.json", fsnotify.Write, true},
		{"zola.toml", fsnotify.Rename, true},
		{"extra/nested/data.txt", fsnotify.Write, true},
		{"custom/file.txt", fsnotify.Write, true},
		{"custom/unrelated.txt", fsnotify.Write, false},
		{"public/index.html", fsnotify.Write, false},
		{"public", fsnotify.Remove, false},
		{"README.md", fsnotify.Write, false},
		{".kopkop-linkcheck-cache.json", fsnotify.Write, false},
		{"content/post.md", fsnotify.Chmod, false},
	} {
		t.Run(tc.name+tc.op.String(), func(t *testing.T) {
			require.Equal(t, tc.want, plan.relevant(fsnotify.Event{Name: filepath.Join(root, tc.name), Op: tc.op}))
		})
	}
	plan = newWatchPlan(s, []string{"."})
	for _, name := range []string{"public/index.html", ".git/index", ".kopkop-serve-stage/index.html"} {
		require.False(t, plan.relevant(fsnotify.Event{Name: filepath.Join(root, name), Op: fsnotify.Write}))
	}
}

func TestWatchPlanRegistersNewDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "content/blog/deep"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "public/nested"), 0o755))
	w, err := fsnotify.NewWatcher()
	require.NoError(t, err)
	defer w.Close()
	s := &site.Site{BasePath: root, ConfigPath: filepath.Join(root, "zola.toml"), OutputPath: filepath.Join(root, "public")}
	plan := newWatchPlan(s, nil)
	require.NoError(t, plan.sync(w))
	require.Contains(t, w.WatchList(), filepath.Join(root, "content/blog/deep"))
	require.NotContains(t, w.WatchList(), filepath.Join(root, "public"))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "templates/new/deep"), 0o755))
	require.NoError(t, plan.sync(w))
	require.Contains(t, w.WatchList(), filepath.Join(root, "templates/new/deep"))
	s.Config.ExtraWatchPaths = []string{"custom"}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "custom/nested"), 0o755))
	require.NoError(t, newWatchPlan(s, nil).sync(w))
	require.Contains(t, w.WatchList(), filepath.Join(root, "custom/nested"))
	s.Config.ExtraWatchPaths = nil
	require.NoError(t, newWatchPlan(s, nil).sync(w))
	require.NotContains(t, w.WatchList(), filepath.Join(root, "custom/nested"))
	require.NoError(t, w.Close())
	require.ErrorContains(t, plan.sync(w), "watch directory")
}
