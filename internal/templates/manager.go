package templates

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Manager struct {
	Engine    *Engine
	Resolver  Resolver
	Available map[string]struct{}
}

func LoadManager(basePath string, theme string) (*Manager, error) {
	eng := NewEngine()
	eng.EnableRelativeTemplateResolution()

	mgr := &Manager{
		Engine:    eng,
		Resolver:  NewResolver(theme),
		Available: map[string]struct{}{},
	}

	if err := mgr.loadTemplatesFrom(filepath.Join(basePath, "templates"), ""); err != nil {
		return nil, err
	}
	if theme != "" {
		themeRoot := filepath.Join(basePath, "themes", theme, "templates")
		if _, err := os.Stat(themeRoot); err == nil {
			if err := mgr.loadTemplatesFrom(themeRoot, fmt.Sprintf("%s/templates", theme)); err != nil {
				return nil, err
			}
		}
	}

	for n, s := range BuiltinTemplates() {
		if err := mgr.Engine.AddTemplate(n, s); err != nil {
			return nil, err
		}
		mgr.Available[n] = struct{}{}
	}

	registerDefaultHelpers(mgr.Engine.Env())
	return mgr, nil
}

func (m *Manager) loadTemplatesFrom(root string, prefix string) error {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		switch ext {
		case ".html", ".xml", ".txt", ".md", ".json", ".ics":
		default:
			return nil
		}

		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if prefix != "" {
			name = prefix + "/" + name
		}

		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := m.Engine.AddTemplate(name, string(b)); err != nil {
			return err
		}
		m.Available[name] = struct{}{}

		if filepath.Base(p) == "robots.txt" {
			if err := m.Engine.AddTemplate("robots.txt", string(b)); err != nil {
				return err
			}
			m.Available["robots.txt"] = struct{}{}
		}
		return nil
	})
}

func (m *Manager) Render(name string, data map[string]any) (string, error) {
	resolved, err := m.Resolver.Resolve(name, m.Available)
	if err != nil {
		return "", err
	}
	return m.Engine.Render(resolved, data)
}

type ShortcodeDefinition struct {
	Name     string
	FileType string
	Template string
}

func (m *Manager) ShortcodeDefinitions() map[string]ShortcodeDefinition {
	defs := map[string]ShortcodeDefinition{}
	names := make([]string, 0, len(m.Available))
	for n := range m.Available {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		fileType := "html"
		if strings.HasSuffix(name, ".md") {
			fileType = "md"
		}
		if strings.HasPrefix(name, "shortcodes/") {
			scName := strings.TrimSuffix(strings.TrimPrefix(name, "shortcodes/"), filepath.Ext(name))
			defs[scName] = ShortcodeDefinition{Name: scName, FileType: fileType, Template: name}
			continue
		}
		if strings.HasPrefix(name, "__zola_builtins/shortcodes/") {
			scName := strings.TrimSuffix(strings.TrimPrefix(name, "__zola_builtins/shortcodes/"), filepath.Ext(name))
			if _, exists := defs[scName]; exists {
				continue
			}
			defs[scName] = ShortcodeDefinition{Name: scName, FileType: fileType, Template: name}
		}
	}
	return defs
}

func registerDefaultHelpers(_ any) {
}
