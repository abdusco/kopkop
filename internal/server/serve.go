package server

import (
	"context"
	"encoding/json"
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
	Debounce        time.Duration
	ExtraWatchPaths []string
	StoreHTML       bool
	BaseURL         string
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
	preview, err := previewURL(s, opts, listener.Addr())
	if err != nil {
		return err
	}
	mount := strings.TrimRight(preview.Path, "/")
	liveAddr := mount + "/__livereload"
	build := func(candidate *site.Site) error {
		started := time.Now()
		candidate.Templates.ReuseURLCache(s.Templates, candidate.Config.LoadURLCacheTTL)
		if err := candidate.Build(site.BuildOptions{IncludeDrafts: opts.IncludeDrafts, BuildMode: site.BuildMemory, BaseURL: preview.String(), LiveReloadURL: liveAddr}); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if opts.StoreHTML {
			if err := storeOutput(candidate); err != nil {
				return err
			}
		}
		log.Printf("build succeeded in %.2fs", time.Since(started).Seconds())
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
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == liveAddr {
			hub.handleWS(w, r)
			return
		}
		snapshotMu.RLock()
		snapshot := current
		snapshotMu.RUnlock()
		serveOutput(snapshot.MemoryOutput, mount, w, r)
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
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
	log.Printf("Serving at %s", preview)
	if opts.OpenBrowser {
		openURL(preview.String())
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
				hub.fail(ctx, err)
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
				hub.fail(ctx, err)
				continue
			}
			snapshotMu.Lock()
			current = candidate
			snapshotMu.Unlock()
			s = candidate
			hub.succeed(ctx)
		}
	}
}

type hub struct {
	conns    map[*websocket.Conn]struct{}
	mu       sync.Mutex
	writeMu  sync.Mutex
	closed   bool
	handlers sync.WaitGroup
	// lastError is the message of the failed build being shown in browsers.
	// Clients that connect while it is set get it immediately.
	lastError string
}

// errorMessage is what the browser overlay receives; plain "reload" is sent on success.
type errorMessage struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// fail tells every connected page to show err instead of reloading.
func (h *hub) fail(ctx context.Context, err error) {
	h.mu.Lock()
	h.lastError = err.Error()
	h.mu.Unlock()
	h.broadcast(ctx, errorPayload(err.Error()))
}

// succeed clears any shown error and reloads every connected page.
func (h *hub) succeed(ctx context.Context) {
	h.mu.Lock()
	h.lastError = ""
	h.mu.Unlock()
	h.broadcast(ctx, "reload")
}

func errorPayload(message string) string {
	b, _ := json.Marshal(errorMessage{Type: "error", Message: message})
	return string(b)
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
	pending := h.lastError
	h.mu.Unlock()
	if pending != "" {
		// Share writeMu with broadcast: gorilla allows one writer per connection.
		h.writeMu.Lock()
		_ = c.SetWriteDeadline(time.Now().Add(time.Second))
		_ = c.WriteMessage(websocket.TextMessage, []byte(errorPayload(pending)))
		h.writeMu.Unlock()
	}
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
