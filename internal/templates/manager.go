package templates

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	minijinja "github.com/mitsuhiko/minijinja/minijinja-go/v2"
	"github.com/mitsuhiko/minijinja/minijinja-go/v2/value"
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

	registerDefaultHelpers(mgr.Engine.Env(), basePath)
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

func registerDefaultHelpers(env *minijinja.Environment, basePath string) {
	env.AddFunction("now", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		_ = state
		_ = args
		_ = kwargs
		return value.FromString(time.Now().UTC().Format(time.RFC3339)), nil
	})

	env.AddFunction("get_url", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		_ = state
		if len(args) == 0 {
			return value.Undefined(), fmt.Errorf("get_url expects at least one argument")
		}
		p, ok := args[0].AsString()
		if !ok {
			return value.Undefined(), fmt.Errorf("get_url expects a string path")
		}
		if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
			return value.FromString(p), nil
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		return value.FromString(p), nil
	})

	env.AddFunction("load_data", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		_ = state
		_ = kwargs
		if len(args) == 0 {
			return value.Undefined(), fmt.Errorf("load_data expects a file path")
		}
		p, ok := args[0].AsString()
		if !ok {
			return value.Undefined(), fmt.Errorf("load_data path must be string")
		}
		abs := filepath.Join(basePath, p)
		b, err := os.ReadFile(abs)
		if err != nil {
			return value.Undefined(), err
		}
		return value.FromString(string(b)), nil
	})

	env.AddFunction("get_hash", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		_ = state
		_ = kwargs
		if len(args) == 0 {
			return value.Undefined(), fmt.Errorf("get_hash expects content or file path")
		}
		raw, ok := args[0].AsString()
		if !ok {
			return value.Undefined(), fmt.Errorf("get_hash first arg must be string")
		}
		data := []byte(raw)
		if st, err := os.Stat(filepath.Join(basePath, raw)); err == nil && !st.IsDir() {
			if b, readErr := os.ReadFile(filepath.Join(basePath, raw)); readErr == nil {
				data = b
			}
		}
		sum := sha256.Sum256(data)
		return value.FromString(fmt.Sprintf("%x", sum[:])), nil
	})

	env.AddFilter("base64_encode", func(state minijinja.FilterState, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		_ = state
		_ = args
		_ = kwargs
		s, ok := val.AsString()
		if !ok {
			return value.Undefined(), fmt.Errorf("base64_encode expects string")
		}
		return value.FromString(base64.StdEncoding.EncodeToString([]byte(s))), nil
	})

	env.AddFilter("base64_decode", func(state minijinja.FilterState, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		_ = state
		_ = args
		_ = kwargs
		s, ok := val.AsString()
		if !ok {
			return value.Undefined(), fmt.Errorf("base64_decode expects string")
		}
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return value.Undefined(), err
		}
		return value.FromString(string(b)), nil
	})

	env.AddFilter("regex_replace", func(state minijinja.FilterState, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		_ = state
		_ = kwargs
		s, ok := val.AsString()
		if !ok {
			return value.Undefined(), fmt.Errorf("regex_replace expects string input")
		}
		if len(args) < 2 {
			return value.Undefined(), fmt.Errorf("regex_replace expects pattern and replacement")
		}
		pattern, ok := args[0].AsString()
		if !ok {
			return value.Undefined(), fmt.Errorf("regex_replace pattern must be string")
		}
		repl, ok := args[1].AsString()
		if !ok {
			return value.Undefined(), fmt.Errorf("regex_replace replacement must be string")
		}
		r, err := regexp.Compile(pattern)
		if err != nil {
			return value.Undefined(), err
		}
		return value.FromString(r.ReplaceAllString(s, repl)), nil
	})
}
