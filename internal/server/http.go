package server

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"syscall"

	"github.com/abdusco/kopkop/internal/filesystem"
	"github.com/abdusco/kopkop/internal/site"
)

func previewURL(s *site.Site, opts ServeOptions, addr net.Addr) (*url.URL, error) {
	cfg := s.Config
	if opts.BaseURL != "" {
		cfg.BaseURL = opts.BaseURL
	} else {
		u, err := url.Parse(cfg.BaseURL)
		if err != nil {
			return nil, err
		}
		host, port, err := net.SplitHostPort(addr.String())
		if err != nil {
			return nil, err
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
			if ip.To4() != nil {
				host = "127.0.0.1"
			} else {
				host = "::1"
			}
		}
		cfg.BaseURL = (&url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: u.Path}).String()
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid preview base URL: %w", err)
	}
	u, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/") + "/")
	if err != nil {
		return nil, err
	}
	if path.Clean(u.Path)+"/" != u.Path && u.Path != "/" {
		return nil, fmt.Errorf("preview base URL path must be canonical")
	}
	return u, nil
}

func serveOutput(output filesystem.FileSystem, mount string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if mount != "" && r.URL.Path == mount {
		redirectDirectory(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, mount+"/") {
		http.NotFound(w, r)
		return
	}
	rel, ok := sanitizeRequestPath(strings.TrimPrefix(r.URL.Path, mount))
	if !ok || (rel != "" && filesystem.ValidatePath(rel) != nil) {
		http.NotFound(w, r)
		return
	}
	if rel == "" || strings.HasSuffix(r.URL.Path, "/") {
		rel = path.Join(rel, "index.html")
	} else if info, err := output.Stat(rel); err == nil && info.IsDir() {
		if info, err := output.Stat(path.Join(rel, "index.html")); err == nil && !info.IsDir() {
			redirectDirectory(w, r)
			return
		}
	}
	data, err := output.ReadFile(rel)
	if err != nil {
		if !isMissing(err) {
			http.Error(w, "cannot read preview output", http.StatusInternalServerError)
			return
		}
		serveNotFound(output, w, r)
		return
	}
	info, err := output.Stat(rel)
	if err != nil {
		http.Error(w, "cannot stat preview output", http.StatusInternalServerError)
		return
	}
	if ct := mime.TypeByExtension(path.Ext(rel)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("ETag", fmt.Sprintf(`"%x"`, sha256.Sum256(data)))
	http.ServeContent(w, r, path.Base(rel), info.ModTime(), bytes.NewReader(data))
}

func isMissing(err error) bool {
	// Opening a directory as a file is also a missing published resource.
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.EISDIR) || errors.Is(err, syscall.ENOTDIR)
}

func redirectDirectory(w http.ResponseWriter, r *http.Request) {
	u := *r.URL
	u.Path += "/"
	u.RawPath = ""
	http.Redirect(w, r, u.String(), http.StatusMovedPermanently)
}

func serveNotFound(output filesystem.FileSystem, w http.ResponseWriter, r *http.Request) {
	data, err := output.ReadFile("404.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	w.WriteHeader(http.StatusNotFound)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func contentType(name string) string {
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}
