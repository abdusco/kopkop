package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/abdusco/kopkop/internal/site"
	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
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
	listener, err := net.Listen("tcp", net.JoinHostPort(opts.Interface, fmt.Sprint(opts.Port)))
	if err != nil {
		return err
	}
	defer listener.Close()
	return run(ctx, s, opts, listener)
}

// run owns the watcher and builds; HTTP handlers only see completed snapshots.
func run(ctx context.Context, s *site.Site, opts ServeOptions, listener net.Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if opts.Debounce <= 0 {
		opts.Debounce = 200 * time.Millisecond
	}
	liveAddr := "ws://" + listener.Addr().String() + "/__livereload"
	build := func(candidate *site.Site) error {
		if err := candidate.Build(site.BuildOptions{IncludeDrafts: opts.IncludeDrafts, BuildMode: site.BuildMemory, LiveReloadURL: liveAddr, Force: true}); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if opts.StoreHTML {
			return storeOutput(candidate)
		}
		return nil
	}
	candidate, err := s.Reload()
	if err != nil {
		return err
	}
	if err := build(candidate); err != nil {
		return err
	}
	s = candidate
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()
	plan := newWatchPlan(s, opts.ExtraWatchPaths)
	if err := plan.sync(watcher); err != nil {
		return err
	}

	hub := newHub()
	defer hub.close()
	var snapshotMu sync.RWMutex
	current := s
	mux := http.NewServeMux()
	mux.HandleFunc("/__livereload", hub.handleWS)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		rel, ok := sanitizeRequestPath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if rel == "" || strings.HasSuffix(r.URL.Path, "/") {
			rel = filepath.Join(rel, "index.html")
		}
		snapshotMu.RLock()
		snapshot := current
		snapshotMu.RUnlock()
		if v, err := snapshot.MemoryOutput.ReadFile(filepath.ToSlash(rel)); err == nil {
			w.Header().Set("Content-Type", contentType(rel))
			_, _ = w.Write(v)
			return
		}
		http.NotFound(w, r)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	served := make(chan error, 1)
	serveFinished := false
	go func() { served <- server.Serve(listener) }()
	defer func() {
		cancel()
		hub.close()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
		}
		if !serveFinished {
			<-served
		}
	}()
	log.Printf("Serving at http://%s", listener.Addr())
	if opts.OpenBrowser {
		openURL("http://" + listener.Addr().String())
	}
	var timer *time.Timer
	var ticks <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-served:
			serveFinished = true
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case event, ok := <-watcher.Events:
			if !ok {
				return fmt.Errorf("watcher events channel closed")
			}
			if !plan.relevant(event) {
				continue
			}
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				if err := plan.sync(watcher); err != nil {
					return err
				}
			}
			if timer == nil {
				timer = time.NewTimer(opts.Debounce)
			} else {
				timer.Stop()
				timer.Reset(opts.Debounce)
			}
			ticks = timer.C
		case err, ok := <-watcher.Errors:
			if !ok {
				return fmt.Errorf("watcher errors channel closed")
			}
			return fmt.Errorf("watcher: %w", err)
		case <-ticks:
			ticks = nil
			candidate, err := s.Reload()
			if err != nil {
				log.Printf("reload configuration/templates error: %v", err)
				continue
			}
			nextPlan := newWatchPlan(candidate, opts.ExtraWatchPaths)
			if err := nextPlan.sync(watcher); err != nil {
				return err
			}
			// Watch newly configured sources even when their first build fails.
			plan = nextPlan
			if err := build(candidate); err != nil {
				log.Printf("reload build error: %v", err)
				continue
			}
			snapshotMu.Lock()
			current = candidate
			snapshotMu.Unlock()
			s = candidate
			hub.broadcast(ctx, "reload")
		}
	}
}

type hub struct {
	conns    map[*websocket.Conn]struct{}
	mu       sync.Mutex
	writeMu  sync.Mutex
	closed   bool
	handlers sync.WaitGroup
}

func newHub() *hub { return &hub{conns: map[*websocket.Conn]struct{}{}} }

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func (h *hub) handleWS(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = c.Close()
		return
	}
	h.conns[c] = struct{}{}
	h.handlers.Add(1)
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.conns, c); h.mu.Unlock(); _ = c.Close(); h.handlers.Done() }()
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			return
		}
	}
}

func (h *hub) broadcast(ctx context.Context, msg string) {
	// Gorilla permits one writer per connection; keep network I/O outside mu.
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	h.mu.Lock()
	connections := make([]*websocket.Conn, 0, len(h.conns))
	for c := range h.conns {
		connections = append(connections, c)
	}
	h.mu.Unlock()
	for _, c := range connections {
		if ctx.Err() != nil {
			return
		}
		err := c.SetWriteDeadline(time.Now().Add(time.Second))
		if err == nil {
			err = c.WriteMessage(websocket.TextMessage, []byte(msg))
		}
		if err != nil {
			h.mu.Lock()
			delete(h.conns, c)
			h.mu.Unlock()
			_ = c.Close()
		}
	}
}

func (h *hub) close() {
	h.mu.Lock()
	h.closed = true
	connections := h.conns
	h.conns = map[*websocket.Conn]struct{}{}
	h.mu.Unlock()
	for c := range connections {
		_ = c.Close()
	}
	h.handlers.Wait()
}

func contentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
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
	if err := cmd.Start(); err == nil {
		go func() { _ = cmd.Wait() }()
	}
}

func sanitizeRequestPath(requestPath string) (string, bool) {
	rel := filepath.Clean(strings.TrimPrefix(requestPath, "/"))
	if strings.HasPrefix(rel, "..") {
		return "", false
	}
	if rel == "." {
		rel = ""
	}
	return rel, true
}
