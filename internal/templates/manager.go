package templates

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	minijinja "github.com/mitsuhiko/minijinja/minijinja-go/v2"
	"github.com/mitsuhiko/minijinja/minijinja-go/v2/value"

	"github.com/abdusco/kopkop/internal/imageproc"
	"github.com/abdusco/kopkop/internal/markdown"
)

var namedEndTagRe = regexp.MustCompile(`\{%(\s*end(?:macro|block))\s+[a-zA-Z0-9_]+\s*%\}`)
var teraMacroCallRe = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)::([a-zA-Z_][a-zA-Z0-9_]*)\(`)

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

	registerDefaultHelpers(mgr.Engine.Env(), basePath, filepath.Join(basePath, "public"))
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
		source := normalizeTemplateSyntax(string(b))
		if err := m.Engine.AddTemplate(name, source); err != nil {
			return fmt.Errorf("parse template %q: %w", name, err)
		}
		m.Available[name] = struct{}{}

		if prefix != "" && !strings.HasPrefix(filepath.ToSlash(rel), "shortcodes/") {
			alias := filepath.ToSlash(rel)
			if _, exists := m.Available[alias]; !exists {
				if err := m.Engine.AddTemplate(alias, source); err != nil {
					return fmt.Errorf("parse template %q: %w", alias, err)
				}
				m.Available[alias] = struct{}{}
			}
		}

		if filepath.Base(p) == "robots.txt" {
			if err := m.Engine.AddTemplate("robots.txt", source); err != nil {
				return err
			}
			m.Available["robots.txt"] = struct{}{}
		}
		return nil
	})
}

func normalizeTemplateSyntax(in string) string {
	// Tera allows named end tags like `{% endmacro name %}`; MiniJinja expects `{% endmacro %}`.
	out := namedEndTagRe.ReplaceAllString(in, `{%$1 %}`)
	out = teraMacroCallRe.ReplaceAllString(out, `${1}.${2}(`)
	return out
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
			continue
		}

		if idx := strings.Index(name, "/templates/shortcodes/"); idx != -1 {
			scName := strings.TrimSuffix(name[idx+len("/templates/shortcodes/"):], filepath.Ext(name))
			if _, exists := defs[scName]; exists {
				continue
			}
			defs[scName] = ShortcodeDefinition{Name: scName, FileType: fileType, Template: name}
		}
	}
	return defs
}

func registerDefaultHelpers(env *minijinja.Environment, basePath string, outputPath string) {
	img := imageproc.New(basePath, outputPath)
	env.AddFunction("now", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		_ = state
		_ = args
		_ = kwargs
		return value.FromString(time.Now().UTC().Format(time.RFC3339)), nil
	})

	env.AddFunction("get_url", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		baseURL := ""
		if cfgVal, ok := state.Lookup("config").AsMap(); ok {
			if bu, ok := cfgVal["base_url"]; ok {
				if s, ok := bu.AsString(); ok {
					baseURL = strings.TrimRight(s, "/")
				}
			}
		}
		p := ""
		if v, ok := kwargs["path"]; ok {
			ps, ok := v.AsString()
			if !ok {
				return value.Undefined(), fmt.Errorf("get_url path must be string")
			}
			p = ps
		} else if len(args) > 0 {
			ps, ok := args[0].AsString()
			if !ok {
				return value.Undefined(), fmt.Errorf("get_url expects a string path")
			}
			p = ps
		} else {
			return value.Undefined(), fmt.Errorf("get_url expects path argument")
		}
		cachebust := false
		if v, ok := kwargs["cachebust"]; ok {
			if b, ok := v.AsBool(); ok {
				cachebust = b
			}
		}

		if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
			return value.FromString(p), nil
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		if cachebust {
			local := strings.TrimPrefix(p, "/")
			candidate := filepath.Join(basePath, "static", local)
			if b, err := os.ReadFile(candidate); err == nil {
				h := sha256.Sum256(b)
				p = p + "?h=" + fmt.Sprintf("%x", h[:10])
			}
		}
		if baseURL != "" {
			return value.FromString(baseURL + p), nil
		}
		return value.FromString(p), nil
	})

	env.AddFunction("get_page", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		pathArg, err := firstPathArg(args, kwargs)
		if err != nil {
			return value.Undefined(), err
		}
		pages := state.Lookup("__pages")
		if m, ok := pages.AsMap(); ok {
			if v, ok := m[pathArg]; ok {
				return v, nil
			}
		}
		return value.Undefined(), fmt.Errorf("page not found: %s", pathArg)
	})

	env.AddFunction("get_section", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		pathArg, err := firstPathArg(args, kwargs)
		if err != nil {
			return value.Undefined(), err
		}
		sections := state.Lookup("__sections")
		if m, ok := sections.AsMap(); ok {
			if v, ok := m[pathArg]; ok {
				return v, nil
			}
		}
		return value.Undefined(), fmt.Errorf("section not found: %s", pathArg)
	})

	env.AddFunction("get_taxonomy", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		_ = args
		kindVal, ok := kwargs["kind"]
		if !ok {
			return value.Undefined(), fmt.Errorf("get_taxonomy expects kind=...")
		}
		kind, ok := kindVal.AsString()
		if !ok {
			return value.Undefined(), fmt.Errorf("get_taxonomy kind must be string")
		}
		taxos := state.Lookup("__taxonomies")
		if m, ok := taxos.AsMap(); ok {
			if v, ok := m[kind]; ok {
				return v, nil
			}
		}
		return value.Undefined(), fmt.Errorf("taxonomy not found: %s", kind)
	})

	env.AddFunction("get_taxonomy_term", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		_ = args
		kindVal, kOK := kwargs["kind"]
		termVal, tOK := kwargs["term"]
		if !kOK || !tOK {
			return value.Undefined(), fmt.Errorf("get_taxonomy_term expects kind=... term=...")
		}
		kind, ok := kindVal.AsString()
		if !ok {
			return value.Undefined(), fmt.Errorf("kind must be string")
		}
		term, ok := termVal.AsString()
		if !ok {
			return value.Undefined(), fmt.Errorf("term must be string")
		}
		taxos := state.Lookup("__taxonomies")
		if m, ok := taxos.AsMap(); ok {
			if v, ok := m[kind]; ok {
				if tm, ok := v.AsMap(); ok {
					if termsV, ok := tm["terms"]; ok {
						if termsMap, ok := termsV.AsMap(); ok {
							if tV, ok := termsMap[term]; ok {
								return tV, nil
							}
						}
					}
				}
			}
		}
		return value.Undefined(), fmt.Errorf("taxonomy term not found: %s/%s", kind, term)
	})

	env.AddFunction("get_taxonomy_url", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		_ = state
		_ = args
		kindVal, kOK := kwargs["kind"]
		termVal, tOK := kwargs["name"]
		if !tOK {
			termVal, tOK = kwargs["term"]
		}
		if !kOK || !tOK {
			return value.Undefined(), fmt.Errorf("get_taxonomy_url expects kind and name/term")
		}
		kind, _ := kindVal.AsString()
		term, _ := termVal.AsString()
		slug := strings.ReplaceAll(strings.ToLower(term), " ", "-")
		return value.FromString("/" + kind + "/" + slug + "/"), nil
	})

	env.AddFunction("trans", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		_ = kwargs
		if len(args) == 0 {
			return value.Undefined(), fmt.Errorf("trans expects key")
		}
		key, ok := args[0].AsString()
		if !ok {
			return value.Undefined(), fmt.Errorf("trans key must be string")
		}
		translations := state.Lookup("__translations")
		lang := ""
		if langV, ok := state.Lookup("lang").AsString(); ok {
			lang = langV
		}
		if m, ok := translations.AsMap(); ok {
			if lv, ok := m[lang]; ok {
				if lm, ok := lv.AsMap(); ok {
					if tv, ok := lm[key]; ok {
						return tv, nil
					}
				}
			}
		}
		return value.FromString(key), nil
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
		base64Out := false
		if b, ok := kwargs["base64"]; ok {
			if bv, ok := b.AsBool(); ok {
				base64Out = bv
			}
		}

		raw := ""
		if p, ok := kwargs["path"]; ok {
			if ps, ok := p.AsString(); ok {
				raw = ps
			}
		}
		if raw == "" && len(args) > 0 {
			if ps, ok := args[0].AsString(); ok {
				raw = ps
			}
		}
		if raw == "" {
			return value.Undefined(), fmt.Errorf("get_hash expects content or file path")
		}
		data := []byte(raw)
		candidates := []string{
			filepath.Join(basePath, raw),
			filepath.Join(basePath, "static", strings.TrimPrefix(raw, "/")),
		}
		for _, candidate := range candidates {
			if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
				if b, readErr := os.ReadFile(candidate); readErr == nil {
					data = b
					break
				}
			}
		}
		sum := sha512.Sum384(data)
		if base64Out {
			return value.FromString(base64.StdEncoding.EncodeToString(sum[:])), nil
		}
		return value.FromString(fmt.Sprintf("%x", sum[:])), nil
	})

	env.AddFunction("get_image_metadata", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		_ = state
		_ = kwargs
		if len(args) == 0 {
			return value.Undefined(), fmt.Errorf("get_image_metadata expects image path")
		}
		p, ok := args[0].AsString()
		if !ok {
			return value.Undefined(), fmt.Errorf("get_image_metadata path must be string")
		}
		md, err := img.GetMetadata(p)
		if err != nil {
			return value.Undefined(), err
		}
		return value.FromMap(map[string]value.Value{
			"width":  value.FromInt(int64(md.Width)),
			"height": value.FromInt(int64(md.Height)),
			"format": value.FromString(md.Format),
		}), nil
	})

	env.AddFunction("resize_image", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		_ = state
		_ = kwargs
		if len(args) < 3 {
			return value.Undefined(), fmt.Errorf("resize_image expects path, width, height")
		}
		p, ok := args[0].AsString()
		if !ok {
			return value.Undefined(), fmt.Errorf("resize_image path must be string")
		}
		w, ok := args[1].AsInt()
		if !ok {
			return value.Undefined(), fmt.Errorf("resize_image width must be int")
		}
		h, ok := args[2].AsInt()
		if !ok {
			return value.Undefined(), fmt.Errorf("resize_image height must be int")
		}
		url, err := img.Resize(p, int(w), int(h))
		if err != nil {
			return value.Undefined(), err
		}
		return value.FromString(url), nil
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

	env.AddFilter("markdown", func(state minijinja.FilterState, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		_ = state
		_ = args
		_ = kwargs
		s, ok := val.AsString()
		if !ok {
			return value.Undefined(), fmt.Errorf("markdown filter expects string")
		}
		res, err := markdown.RenderContent(s, markdown.RenderContext{})
		if err != nil {
			return value.Undefined(), err
		}
		return value.FromSafeString(res.Body), nil
	})

	env.AddFilter("num_format", func(state minijinja.FilterState, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		_ = state
		_ = kwargs
		f, ok := val.AsFloat()
		if !ok {
			if i, ok := val.AsInt(); ok {
				f = float64(i)
			} else {
				return value.Undefined(), fmt.Errorf("num_format expects number")
			}
		}
		precision := 2
		if len(args) > 0 {
			if p, ok := args[0].AsInt(); ok {
				precision = int(p)
			}
		}
		pow := math.Pow10(precision)
		v := math.Round(f*pow) / pow
		return value.FromString(fmt.Sprintf("%.*f", precision, v)), nil
	})

	env.AddFilter("default", func(state minijinja.FilterState, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		_ = state

		fallback, hasFallback := kwargs["value"]
		if !hasFallback && len(args) > 0 {
			fallback = args[0]
			hasFallback = true
		}
		if !hasFallback {
			return value.Undefined(), fmt.Errorf("default expects value")
		}

		strict := false
		if v, ok := kwargs["boolean"]; ok {
			if b, ok := v.AsBool(); ok {
				strict = b
			}
		} else if len(args) > 1 {
			if b, ok := args[1].AsBool(); ok {
				strict = b
			}
		}

		if val.IsUndefined() {
			return fallback, nil
		}
		if !strict {
			if s, ok := val.AsString(); ok && s == "" {
				return fallback, nil
			}
		}
		return val, nil
	})
}

func firstPathArg(args []value.Value, kwargs map[string]value.Value) (string, error) {
	if v, ok := kwargs["path"]; ok {
		if s, ok := v.AsString(); ok {
			return s, nil
		}
		return "", fmt.Errorf("path must be string")
	}
	if len(args) > 0 {
		if s, ok := args[0].AsString(); ok {
			return s, nil
		}
		return "", fmt.Errorf("path must be string")
	}
	return "", fmt.Errorf("missing path argument")
}
