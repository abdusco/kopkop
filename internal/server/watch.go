package server

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/abdusco/kopkop/internal/site"
	"github.com/fsnotify/fsnotify"
)

type watchPlan struct {
	roots    []string
	config   string
	excluded []string
}

func newWatchPlan(s *site.Site, extra []string) watchPlan {
	abs := func(p string) string {
		if !filepath.IsAbs(p) {
			p = filepath.Join(s.BasePath, p)
		}
		p, _ = filepath.Abs(p)
		return filepath.Clean(p)
	}
	configPath, _ := filepath.Abs(s.ConfigPath)
	p := watchPlan{config: configPath}
	for _, name := range []string{"content", "templates", "static", "data", "themes"} {
		p.roots = append(p.roots, abs(name))
	}
	for _, name := range append(append([]string{}, extra...), s.Config.ExtraWatchPaths...) {
		p.roots = append(p.roots, abs(name))
	}
	outputPath, _ := filepath.Abs(s.OutputPath)
	p.excluded = []string{outputPath, abs(".git"), abs(".agents"), abs(".codex")}
	if s.Config.LinkChecker.CacheFile != "" {
		p.excluded = append(p.excluded, abs(s.Config.LinkChecker.CacheFile))
	}
	return p
}

func within(root, name string) bool {
	rel, err := filepath.Rel(root, name)
	return err == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (p watchPlan) ignored(name string) bool {
	for part := name; filepath.Dir(part) != part; part = filepath.Dir(part) {
		if strings.HasPrefix(filepath.Base(part), ".kopkop-serve-") {
			return true
		}
	}
	for _, root := range p.excluded {
		if within(root, name) {
			return true
		}
	}
	return false
}

func (p watchPlan) relevant(event fsnotify.Event) bool {
	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
		return false
	}
	name := filepath.Clean(event.Name)
	if p.ignored(name) {
		return false
	}
	if name == p.config {
		return true
	}
	for _, root := range p.roots {
		if within(root, name) || within(name, root) {
			return true
		}
	}
	return false
}

// sync registers existing directories recursively and the nearest existing
// parent of missing roots, so atomic file replacements and new trees are seen.
func (p watchPlan) sync(w *fsnotify.Watcher) error {
	wanted := map[string]bool{}
	addParent := func(name string) error {
		for parent := filepath.Dir(name); ; parent = filepath.Dir(parent) {
			info, err := os.Stat(parent)
			if err == nil {
				if !info.IsDir() {
					return fmt.Errorf("watch parent %q is not a directory", parent)
				}
				wanted[parent] = true
				return nil
			}
			if !os.IsNotExist(err) {
				return fmt.Errorf("inspect watch parent %q: %w", parent, err)
			}
			if filepath.Dir(parent) == parent {
				return err
			}
		}
	}
	if err := addParent(p.config); err != nil {
		return err
	}
	for _, root := range p.roots {
		if p.ignored(root) {
			continue
		}
		if err := addParent(root); err != nil {
			return err
		}
		if err := filepath.WalkDir(root, func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if p.ignored(name) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				wanted[name] = true
			}
			return nil
		}); err != nil {
			return fmt.Errorf("scan watch root %q: %w", root, err)
		}
	}
	current := map[string]bool{}
	for _, name := range w.WatchList() {
		current[name] = true
	}
	for name := range wanted {
		if !current[name] {
			if err := w.Add(name); err != nil {
				return fmt.Errorf("watch directory %q: %w", name, err)
			}
		}
	}
	for name := range current {
		if !wanted[name] {
			if err := w.Remove(name); err != nil && !os.IsNotExist(err) && !errors.Is(err, fsnotify.ErrNonExistentWatch) {
				return fmt.Errorf("unwatch directory %q: %w", name, err)
			}
		}
	}
	return nil
}
