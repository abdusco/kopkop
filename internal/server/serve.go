package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"

	"github.com/abdusco/kopkop/internal/site"
)

type ServeOptions struct {
	Interface       string
	Port            int
	IncludeDrafts   bool
	OpenBrowser     bool
	Fast            bool
	Debounce        time.Duration
	ExtraWatchPaths []string
	StoreHTML       bool
	Force           bool
}

func Run(ctx context.Context, s *site.Site, opts ServeOptions) error {
	if opts.Port == 0 {
		opts.Port = 1111
	}
	if opts.Interface == "" {
		opts.Interface = "127.0.0.1"
	}
	if opts.Debounce <= 0 {
		opts.Debounce = 200 * time.Millisecond
	}

	liveAddr := fmt.Sprintf("ws://%s:%d/__livereload", opts.Interface, opts.Port)
	bMode := site.BuildMemory
	if opts.StoreHTML {
		bMode = site.BuildBoth
	}
	if err := s.Build(site.BuildOptions{
		IncludeDrafts: opts.IncludeDrafts,
		BuildMode:     bMode,
		LiveReloadURL: liveAddr,
		Force:         true,
		Minify:        false,
	}); err != nil {
		return err
	}

	hub := newHub()
	go hub.run()

	mux := http.NewServeMux()
	mux.HandleFunc("/__livereload", hub.handleWS)
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel, ok := sanitizeRequestPath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if rel == "" || strings.HasSuffix(r.URL.Path, "/") {
			rel = filepath.Join(rel, "index.html")
		}
		if s.MemoryOutput != nil {
			if v, err := s.MemoryOutput.ReadFile(filepath.ToSlash(rel)); err == nil {
				w.Header().Set("Content-Type", contentType(rel))
				_, _ = w.Write(v)
				return
			}
		}
		http.FileServer(http.Dir(s.OutputPath)).ServeHTTP(w, r)
	}))

	server := &http.Server{Addr: fmt.Sprintf("%s:%d", opts.Interface, opts.Port), Handler: mux}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	watchPaths := []string{
		filepath.Join(s.BasePath, "content"),
		filepath.Join(s.BasePath, "templates"),
		filepath.Join(s.BasePath, "static"),
		filepath.Dir(s.ConfigPath),
	}
	watchPaths = append(watchPaths, opts.ExtraWatchPaths...)
	watchPaths = append(watchPaths, s.Config.ExtraWatchPaths...)

	for _, p := range watchPaths {
		if stat, statErr := os.Stat(p); statErr == nil && stat.IsDir() {
			_ = watcher.Add(p)
		}
	}

	rebuildTrigger := make(chan struct{}, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-rebuildTrigger:
				for {
					if err := s.Load(opts.IncludeDrafts); err != nil {
						log.Printf("reload load error: %v", err)
						break
					}
					if err := s.Build(site.BuildOptions{
						IncludeDrafts: opts.IncludeDrafts,
						BuildMode:     bMode,
						LiveReloadURL: liveAddr,
						Force:         true,
					}); err != nil {
						log.Printf("reload build error: %v", err)
						break
					}
					hub.broadcast("reload")

					hasPending := false
					for {
						select {
						case <-rebuildTrigger:
							hasPending = true
						default:
							if !hasPending {
								goto workerDone
							}
							goto nextBuild
						}
					}
				nextBuild:
				}
			workerDone:
			}
		}
	}()

	go func() {
		var timer *time.Timer
		for {
			select {
			case <-ctx.Done():
				return
			case <-watcher.Events:
				if timer == nil {
					timer = time.NewTimer(opts.Debounce)
				} else {
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					timer.Reset(opts.Debounce)
				}
			case err := <-watcher.Errors:
				log.Printf("watcher error: %v", err)
			default:
				if timer != nil {
					select {
					case <-timer.C:
						select {
						case rebuildTrigger <- struct{}{}:
						default:
						}
						timer = nil
					default:
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()

	log.Printf("Serving at http://%s:%d", opts.Interface, opts.Port)
	if opts.OpenBrowser {
		openURL(fmt.Sprintf("http://%s:%d", opts.Interface, opts.Port))
	}
	return server.ListenAndServe()
}

type hub struct {
	conns map[*websocket.Conn]struct{}
	mu    sync.Mutex
}

func newHub() *hub { return &hub{conns: map[*websocket.Conn]struct{}{}} }

func (h *hub) run() {}

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func (h *hub) handleWS(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	h.mu.Lock()
	h.conns[c] = struct{}{}
	h.mu.Unlock()

	for {
		if _, _, err := c.ReadMessage(); err != nil {
			break
		}
	}
	h.mu.Lock()
	delete(h.conns, c)
	h.mu.Unlock()
	_ = c.Close()
}

func (h *hub) broadcast(msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns {
		_ = c.WriteMessage(websocket.TextMessage, []byte(msg))
	}
}

func contentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".txt":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func openURL(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	_ = cmd.Start()
}

func sanitizeRequestPath(requestPath string) (string, bool) {
	rel := strings.TrimPrefix(requestPath, "/")
	rel = filepath.Clean(rel)
	if strings.HasPrefix(rel, "..") {
		return "", false
	}
	if rel == "." {
		rel = ""
	}
	return rel, true
}
