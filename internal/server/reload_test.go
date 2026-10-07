package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abdusco/kopkop/internal/site"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestHubReplaysBuildErrorToNewClients(t *testing.T) {
	t.Parallel()

	h := newHub()
	defer h.close()
	srv := httptest.NewServer(http.HandlerFunc(h.handleWS))
	defer srv.Close()
	dial := func() *websocket.Conn {
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
		require.NoError(t, err)
		return conn
	}
	read := func(conn *websocket.Conn) string {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		_, msg, err := conn.ReadMessage()
		require.NoError(t, err)
		return string(msg)
	}

	h.fail(context.Background(), errors.New("render page \"a.md\": boom\nsecond line"))

	// A page opened while the build is broken gets the error immediately.
	late := dial()
	defer late.Close()
	var failure errorMessage
	require.NoError(t, json.Unmarshal([]byte(read(late)), &failure))
	require.Equal(t, errorMessage{Type: "error", Message: "render page \"a.md\": boom\nsecond line"}, failure)

	// A successful build clears the error: new clients get nothing, open ones reload.
	h.succeed(context.Background())
	require.Equal(t, "reload", read(late))
	fresh := dial()
	defer fresh.Close()
	require.NoError(t, fresh.SetReadDeadline(time.Now().Add(100*time.Millisecond)))
	_, _, err := fresh.ReadMessage()
	require.Error(t, err)
}

func TestServeReloadAndShutdown(t *testing.T) {
	t.Parallel()
	for _, store := range []bool{false, true} {
		t.Run(fmt.Sprintf("store=%v", store), func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			files := map[string]string{
				"zola.toml":            "base_url='https://example.com'\ntitle='Initial'\nbuild_search_index=true\n",
				"content/blog/post.md": "First body",
				"templates/page.html":  "{{ config.title }}:{{ page.content | safe }}",
				"static/css/style.css": "old-style",
				"extra/info.txt":       "original-data",
			}
			for name, body := range files {
				p := filepath.Join(root, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
				require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
			}
			s, err := site.New(site.SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "zola.toml")})
			require.NoError(t, err)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			finished := make(chan error, 1)
			go func() {
				finished <- run(ctx, s, ServeOptions{Debounce: 20 * time.Millisecond, StoreHTML: store}, listener)
			}()
			t.Cleanup(func() { cancel(); _ = listener.Close() })
			baseURL := "http://" + listener.Addr().String()
			client := &http.Client{Timeout: time.Second}
			defer client.CloseIdleConnections()
			fetch := func(path string) string {
				resp, err := client.Get(baseURL + path)
				if err != nil {
					return ""
				}
				defer resp.Body.Close()
				body, _ := io.ReadAll(resp.Body)
				return string(body)
			}
			require.Eventually(t, func() bool { return strings.Contains(fetch("/blog/post/"), "Initial:") }, 5*time.Second, 10*time.Millisecond)
			conn, _, err := websocket.DefaultDialer.Dial("ws://"+listener.Addr().String()+"/__livereload", nil)
			require.NoError(t, err)
			defer conn.Close()
			reloaded := make(chan string, 32)
			wsDone := make(chan struct{})
			go func() {
				defer close(wsDone)
				for {
					_, msg, err := conn.ReadMessage()
					if err != nil {
						return
					}
					reloaded <- string(msg)
				}
			}()
			// Keep requests running while snapshots are rebuilt; the race detector
			// checks publication alongside real HTTP requests.
			var requests sync.WaitGroup
			requests.Add(1)
			go func() {
				defer requests.Done()
				for ctx.Err() == nil {
					fetch("/blog/post/")
				}
			}()
			defer func() { cancel(); requests.Wait() }()
			for _, tc := range []struct{ name, path, body, url, want string }{
				{"nested content", "content/blog/post.md", "Second body", "/blog/post/", "Second body"},
				{"template", "templates/page.html", "Changed:{{ config.title }}:{{ page.content | safe }}", "/blog/post/", "Changed:Initial"},
				{"configuration", "zola.toml", "base_url='https://changed.example'\ntitle='Updated'\nbuild_search_index=true\nextra_watch_paths=['extra']\n", "/blog/post/", "Changed:Updated"},
				{"new nested tree", "content/new/deep/post.md", "New nested body", "/new/deep/post/", "New nested body"},
				{"edit new nested tree", "content/new/deep/post.md", "Edited nested body", "/new/deep/post/", "Edited nested body"},
				{"nested static", "static/css/style.css", "new-style", "/css/style.css", "new-style"},
				{"extra data template", "templates/page.html", "Changed:{{ config.title }}:{{ load_data('extra/info.txt') }}:{{ page.content | safe }}", "/blog/post/", "original-data"},
				{"configured extra path", "extra/info.txt", "updated-data", "/blog/post/", "updated-data"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					p := filepath.Join(root, tc.path)
					require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
					require.NoError(t, os.WriteFile(p, []byte(tc.body), 0o644))
					require.Eventually(t, func() bool { return strings.Contains(fetch(tc.url), tc.want) }, 5*time.Second, 10*time.Millisecond)
					select {
					case msg := <-reloaded:
						require.Equal(t, "reload", msg)
					case <-time.After(5 * time.Second):
						t.Fatal("no reload notification")
					}
				})
			}
			require.Contains(t, fetch("/search_index.json"), baseURL)
			require.NotContains(t, fetch("/search_index.json"), "changed.example")
			for _, tc := range []struct{ name, path, body string }{
				{"bad template", "templates/page.html", "{{ broken() }}"},
				{"bad config", "zola.toml", "invalid = ["},
			} {
				t.Run(tc.name, func(t *testing.T) {
					require.NoError(t, os.WriteFile(filepath.Join(root, tc.path), []byte(tc.body), 0o644))
					// Pages are told about the failure instead of reloading.
					select {
					case msg := <-reloaded:
						var failure errorMessage
						require.NoError(t, json.Unmarshal([]byte(msg), &failure))
						require.Equal(t, "error", failure.Type)
						require.NotEmpty(t, failure.Message)
					case <-time.After(5 * time.Second):
						t.Fatal("no error notification")
					}
					require.Never(t, func() bool { return !strings.Contains(fetch("/blog/post/"), "Changed:Updated") }, 200*time.Millisecond, 10*time.Millisecond)
					if store {
						data, err := os.ReadFile(filepath.Join(root, "public/blog/post/index.html"))
						require.NoError(t, err)
						require.Contains(t, string(data), "Changed:Updated")
					}
				})
			}
			// Recover configuration via atomic replacement, then repair the template.
			require.NoError(t, os.WriteFile(filepath.Join(root, "config.tmp"), []byte("base_url='https://example.com'\ntitle='Recovered'\n"), 0o644))
			require.NoError(t, os.Rename(filepath.Join(root, "config.tmp"), filepath.Join(root, "zola.toml")))
			require.NoError(t, os.WriteFile(filepath.Join(root, "templates/page.html"), []byte("{{ config.title }}:{{ page.content | safe }}"), 0o644))
			require.Eventually(t, func() bool { return strings.Contains(fetch("/blog/post/"), "Recovered:") }, 5*time.Second, 10*time.Millisecond)
			require.NoError(t, os.MkdirAll(filepath.Join(root, "themes/demo/templates"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(root, "themes/demo/theme.toml"), []byte("name='demo'\n"), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(root, "themes/demo/templates/page.html"), []byte("Theme:{{ page.content | safe }}"), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(root, "zola.toml"), []byte("base_url='https://example.com'\ntheme='demo'\n"), 0o644))
			require.NoError(t, os.Remove(filepath.Join(root, "templates/page.html")))
			require.Eventually(t, func() bool { return strings.Contains(fetch("/blog/post/"), "Theme:") }, 5*time.Second, 10*time.Millisecond)
			require.NoError(t, os.WriteFile(filepath.Join(root, "themes/demo/templates/page.html"), []byte("Updated theme:{{ page.content | safe }}"), 0o644))
			require.Eventually(t, func() bool { return strings.Contains(fetch("/blog/post/"), "Updated theme:") }, 5*time.Second, 10*time.Millisecond)
			if !store {
				require.NoDirExists(t, filepath.Join(root, "public"))
			}
			cancel()
			select {
			case err := <-finished:
				require.NoError(t, err)
			case <-time.After(6 * time.Second):
				t.Fatal("server failed to stop")
			}
			select {
			case <-wsDone:
			case <-time.After(time.Second):
				t.Fatal("websocket failed to close")
			}
			requests.Wait()
		})
	}
}

func TestServePreviewSubpath(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"zola.toml":           "base_url='https://production.example/old/'\n",
		"content/post.md":     "Body",
		"templates/page.html": `{{ config.base_url }}|{{ page.permalink }}|{{ page.content | safe }}`,
	} {
		filename := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0o755))
		require.NoError(t, os.WriteFile(filename, []byte(body), 0o644))
	}
	s, err := site.New(site.SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "zola.toml")})
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base := "http://" + listener.Addr().String()
	finished := make(chan error, 1)
	go func() { finished <- run(ctx, s, ServeOptions{BaseURL: base + "/preview/"}, listener) }()
	t.Cleanup(func() { cancel(); _ = listener.Close() })
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	var body string
	require.Eventually(t, func() bool {
		resp, err := client.Get(base + "/preview/post/")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		body = string(data)
		return err == nil && resp.StatusCode == 200
	}, 5*time.Second, 10*time.Millisecond)
	require.Contains(t, body, base+"/preview/post/")
	require.NotContains(t, body, "production.example")
	require.Contains(t, body, `new URL("/preview/__livereload",window.location.href)`)
	require.Contains(t, body, `location.protocol === 'https:' ? 'wss:' : 'ws:'`)
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+listener.Addr().String()+"/preview/__livereload", nil)
	require.NoError(t, err)
	defer conn.Close()
	resp, err := client.Get(base + "/post/")
	require.NoError(t, err)
	require.Equal(t, 404, resp.StatusCode)
	_ = resp.Body.Close()
	cancel()
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("server failed to stop")
	}
}

func TestRunBindFailure(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	// Binding happens before starting builds or workers.
	require.Error(t, Run(context.Background(), nil, ServeOptions{Interface: "127.0.0.1", Port: port}))
}
