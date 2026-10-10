package templates

import (
	"cmp"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	minijinja "github.com/mitsuhiko/minijinja/minijinja-go/v2"
	"github.com/mitsuhiko/minijinja/minijinja-go/v2/value"
	"github.com/samber/lo"
	"gopkg.in/yaml.v3"

	"github.com/abdusco/kopkop/internal/filesystem"
	"github.com/abdusco/kopkop/internal/imageproc"
	"github.com/abdusco/kopkop/internal/markdown"
	"github.com/abdusco/kopkop/internal/slug"
)

var teraHTMLEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&#x27;",
	"/", "&#x2F;",
)

var namedEndTagRe = regexp.MustCompile(`\{%(\s*end(?:macro|block))\s+[a-zA-Z0-9_]+\s*%\}`)
var templateTagRe = regexp.MustCompile(`(?s)\{\{.*?\}\}|\{%.*?%\}`)
var stringLiteralRe = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'`)
var teraMacroCallRe = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)::([a-zA-Z_][a-zA-Z0-9_]*)\(`)

type urlCacheEntry struct {
	body        []byte
	contentType string
}

type urlLoad struct {
	done  chan struct{}
	entry urlCacheEntry
	err   error
}

type responseCache struct {
	mu    sync.Mutex
	loads map[string]*urlLoad
}

func (c *responseCache) Load(req *http.Request, body string) (urlCacheEntry, error) {
	identity, _ := json.Marshal([]any{req.Method, req.URL.String(), body, req.Header})
	key := fmt.Sprintf("%x", sha256.Sum256(identity))
	c.mu.Lock()
	if existing := c.loads[key]; existing != nil {
		c.mu.Unlock()
		<-existing.done
		return existing.entry, existing.err
	}
	if c.loads == nil {
		c.loads = map[string]*urlLoad{}
	}
	load := &urlLoad{done: make(chan struct{})}
	c.loads[key] = load
	c.mu.Unlock()
	load.entry, load.err = fetchURL(req)
	c.mu.Lock()
	if load.err != nil {
		delete(c.loads, key)
	}
	close(load.done)
	c.mu.Unlock()
	return load.entry, load.err
}

func fetchURL(req *http.Request) (urlCacheEntry, error) {
	timeout, err := loadURLTimeout()
	if err != nil {
		return urlCacheEntry{}, err
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return urlCacheEntry{}, fmt.Errorf("load_url: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return urlCacheEntry{}, fmt.Errorf("load_url: HTTP %d for %s", resp.StatusCode, req.URL)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxLoadURLBytes+1))
	if err != nil {
		return urlCacheEntry{}, fmt.Errorf("load_url: reading response: %w", err)
	}
	if len(b) > maxLoadURLBytes {
		return urlCacheEntry{}, fmt.Errorf("load_url: response for %s exceeds %d bytes", req.URL, maxLoadURLBytes)
	}
	return urlCacheEntry{body: b, contentType: resp.Header.Get("Content-Type")}, nil
}

func urlFormat(explicit string, u *url.URL, ct string) string {
	if explicit != "" {
		return strings.ToLower(explicit)
	}
	if format := contentTypeToFormat(ct); format != "" {
		return format
	}
	return strings.TrimPrefix(strings.ToLower(path.Ext(u.Path)), ".")
}

func contentTypeToFormat(ct string) string {
	ct = strings.SplitN(ct, ";", 2)[0]
	ct = strings.ToLower(strings.TrimSpace(ct))
	if strings.HasSuffix(ct, "+json") {
		return "json"
	}
	switch ct {
	case "application/json":
		return "json"
	case "application/toml", "text/x-toml":
		return "toml"
	case "application/yaml", "text/yaml", "application/x-yaml", "text/x-yaml":
		return "yaml"
	case "text/csv":
		return "csv"
	default:
		return ""
	}
}

const (
	defaultLoadURLTimeout = 30 * time.Second
	maxLoadURLBytes       = 64 << 20
)

// loadURLTimeout reads LOAD_URL_TIMEOUT (a Go duration such as "5s"). It is an
// environment setting because it is about the build machine's network, not the site.
func loadURLTimeout() (time.Duration, error) {
	v := os.Getenv("LOAD_URL_TIMEOUT")
	if v == "" {
		return defaultLoadURLTimeout, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("load_url: invalid LOAD_URL_TIMEOUT %q: want a positive duration like 5s", v)
	}
	return d, nil
}

func parseData(b []byte, format string) (value.Value, error) {
	switch format {
	case "json":
		var parsed any
		if err := json.Unmarshal(b, &parsed); err != nil {
			return value.Undefined(), fmt.Errorf("JSON parse error: %w", err)
		}
		return value.FromAny(parsed), nil
	case "toml":
		var parsed any
		if err := toml.Unmarshal(b, &parsed); err != nil {
			return value.Undefined(), fmt.Errorf("TOML parse error: %w", err)
		}
		return value.FromAny(parsed), nil
	case "yaml", "yml":
		var parsed any
		if err := yaml.Unmarshal(b, &parsed); err != nil {
			return value.Undefined(), fmt.Errorf("YAML parse error: %w", err)
		}
		return value.FromAny(parsed), nil
	case "csv":
		r := csv.NewReader(strings.NewReader(string(b)))
		records, err := r.ReadAll()
		if err != nil {
			return value.Undefined(), fmt.Errorf("CSV parse error: %w", err)
		}
		headers := []value.Value{}
		dataRecords := []value.Value{}
		if len(records) > 0 {
			for _, h := range records[0] {
				headers = append(headers, value.FromString(h))
			}
			for _, row := range records[1:] {
				cells := make([]value.Value, len(row))
				for i, cell := range row {
					cells[i] = value.FromString(cell)
				}
				dataRecords = append(dataRecords, value.FromSlice(cells))
			}
		}
		return value.FromMap(map[string]value.Value{
			"headers": value.FromSlice(headers),
			"records": value.FromSlice(dataRecords),
		}), nil
	default:
		return value.FromString(string(b)), nil
	}
}

type Manager struct {
	Engine    *Engine
	Resolver  Resolver
	Available map[string]struct{}
	SourceFS  filesystem.FileSystem
	OutputFS  filesystem.FileSystem
	// ColocatedAssets maps published asset paths to paths in SourceFS.
	ColocatedAssets map[string]string
}

func LoadManager(basePath string, theme string) (*Manager, error) {
	return LoadManagerFS(filesystem.NewDiskFS(basePath), filesystem.NewDiskFS(filepath.Join(basePath, "public")), theme)
}

func LoadManagerFS(sourceFS filesystem.FileSystem, outputFS filesystem.FileSystem, theme string) (*Manager, error) {
	if theme != "" {
		if err := filesystem.ValidatePath(theme); err != nil {
			return nil, fmt.Errorf("invalid theme path: %w", err)
		}
	}
	eng := NewEngine()
	eng.EnableRelativeTemplateResolution()

	mgr := &Manager{
		Engine:    eng,
		Resolver:  NewResolver(theme),
		Available: map[string]struct{}{},
		SourceFS:  sourceFS,
		OutputFS:  outputFS,
	}

	if err := mgr.loadTemplatesFrom(path.Join("templates"), ""); err != nil {
		return nil, err
	}
	if theme != "" {
		themeRoot := path.Join("themes", theme, "templates")
		if _, err := fs.Stat(sourceFS, themeRoot); err == nil {
			if err := mgr.loadTemplatesFrom(themeRoot, fmt.Sprintf("%s/templates", theme)); err != nil {
				return nil, err
			}
		}
	}

	for name, content := range BuiltinTemplates() {
		if err := mgr.Engine.AddTemplate(name, content); err != nil {
			return nil, err
		}
		mgr.Available[name] = struct{}{}
	}

	mgr.ConfigureHelpers()
	return mgr, nil
}

func (m *Manager) ConfigureHelpers() {
	registerDefaultHelpers(m.Engine.Env(), m.SourceFS, m.OutputFS, m.ColocatedAssets)
}

func (m *Manager) loadTemplatesFrom(root string, prefix string) error {
	if _, err := fs.Stat(m.SourceFS, root); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return fs.WalkDir(m.SourceFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(path.Ext(p))
		switch ext {
		case ".html", ".xml", ".txt", ".md", ".json", ".ics":
		default:
			return nil
		}

		rel := strings.TrimPrefix(strings.TrimPrefix(p, root), "/")
		name := rel
		if prefix != "" {
			name = prefix + "/" + name
		}

		b, err := fs.ReadFile(m.SourceFS, p)
		if err != nil {
			return err
		}
		source := normalizeTemplateSyntax(string(b))
		if err := m.Engine.AddTemplate(name, source); err != nil {
			return fmt.Errorf("parse template %q: %w", name, err)
		}
		m.Available[name] = struct{}{}

		if prefix != "" && !strings.HasPrefix(rel, "shortcodes/") {
			alias := rel
			if _, exists := m.Available[alias]; !exists {
				if err := m.Engine.AddTemplate(alias, source); err != nil {
					return fmt.Errorf("parse template %q: %w", alias, err)
				}
				m.Available[alias] = struct{}{}
			}
		}

		if path.Base(p) == "robots.txt" {
			if err := m.Engine.AddTemplate("robots.txt", source); err != nil {
				return err
			}
			m.Available["robots.txt"] = struct{}{}
		}
		return nil
	})
}

// normalizeTemplateSyntax rewrites Tera-only syntax inside template tags. Text
// outside `{{ }}` and `{% %}` and string literals inside them stay untouched.
func normalizeTemplateSyntax(in string) string {
	return templateTagRe.ReplaceAllStringFunc(in, func(tag string) string {
		// Tera allows named end tags like `{% endmacro name %}`; MiniJinja expects `{% endmacro %}`.
		tag = namedEndTagRe.ReplaceAllString(tag, `{%$1 %}`)
		var out strings.Builder
		last := 0
		for _, loc := range stringLiteralRe.FindAllStringIndex(tag, -1) {
			out.WriteString(teraMacroCallRe.ReplaceAllString(tag[last:loc[0]], `${1}.${2}(`))
			out.WriteString(tag[loc[0]:loc[1]])
			last = loc[1]
		}
		out.WriteString(teraMacroCallRe.ReplaceAllString(tag[last:], `${1}.${2}(`))
		return out.String()
	})
}

func (m *Manager) Render(name string, data map[string]any) (string, error) {
	resolved, err := m.Resolver.Resolve(name, m.Available)
	if err != nil {
		return "", err
	}
	out, err := m.Engine.Render(resolved, data)
	if err != nil {
		return "", fmt.Errorf("render template %q: %w", resolved, err)
	}
	return out, nil
}

type ShortcodeDefinition struct {
	Name     string
	FileType string
	Template string
}

func (m *Manager) ShortcodeDefinitions() map[string]ShortcodeDefinition {
	defs := map[string]ShortcodeDefinition{}
	names := lo.Keys(m.Available)
	sort.Strings(names)
	for _, name := range names {
		fileType := "html"
		if strings.HasSuffix(name, ".md") {
			fileType = "md"
		}
		if rest, ok := strings.CutPrefix(name, "shortcodes/"); ok {
			scName := strings.TrimSuffix(rest, filepath.Ext(rest))
			defs[scName] = ShortcodeDefinition{Name: scName, FileType: fileType, Template: name}
			continue
		}
		if rest, ok := strings.CutPrefix(name, "__kopkop_builtins/shortcodes/"); ok {
			scName := strings.TrimSuffix(rest, filepath.Ext(rest))
			if _, exists := defs[scName]; exists {
				continue
			}
			defs[scName] = ShortcodeDefinition{Name: scName, FileType: fileType, Template: name}
			continue
		}

		if _, rest, ok := strings.Cut(name, "/templates/shortcodes/"); ok {
			scName := strings.TrimSuffix(rest, filepath.Ext(rest))
			defs[scName] = ShortcodeDefinition{Name: scName, FileType: fileType, Template: name}
		}
	}
	return defs
}

func registerDefaultHelpers(env *minijinja.Environment, sourceFS filesystem.FileSystem, outputFS filesystem.FileSystem, colocatedAssets map[string]string) {
	img := imageproc.New(sourceFS, outputFS)
	env.AddFunction("now", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		return value.FromString(time.Now().UTC().Format(time.RFC3339)), nil
	})

	env.AddFunction("get_env", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		nameVal, ok := kwargs["name"]
		if !ok {
			return value.Undefined(), fmt.Errorf("get_env expects name=...")
		}
		name, ok := nameVal.AsString()
		if !ok {
			return value.Undefined(), fmt.Errorf("get_env name must be string")
		}
		def := ""
		if d, ok := kwargs["default"]; ok {
			if s, ok := d.AsString(); ok {
				def = s
			}
		}
		if val, exists := os.LookupEnv(name); exists {
			return value.FromString(val), nil
		}
		return value.FromString(def), nil
	})

	env.AddFunction("get_url", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		baseURL := configBaseURL(state)
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
		absolute := false
		if cfgVal, ok := state.Lookup("config").AsMap(); ok {
			if ls, ok := cfgVal["link_strategy"]; ok {
				if s, ok := ls.AsString(); ok {
					absolute = strings.EqualFold(strings.TrimSpace(s), "absolute")
				}
			} else {
				absolute = true
			}
		} else {
			absolute = true
		}
		if v, ok := kwargs["absolute"]; ok {
			b, ok := v.AsBool()
			if !ok {
				return value.Undefined(), fmt.Errorf("get_url absolute must be bool")
			}
			absolute = b
		}

		if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
			return value.FromSafeString(p), nil
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		if cachebust {
			local := filepath.ToSlash(strings.TrimPrefix(p, "/"))
			var b []byte
			var err error
			if source, ok := colocatedAssets[local]; ok {
				// These assets are copied after rendering. Always read the source,
				// including when an earlier build's output is still present.
				b, err = sourceFS.ReadFile(source)
			} else {
				b, err = outputFS.ReadFile(local)
				if err != nil {
					b, err = fs.ReadFile(sourceFS, path.Join("static", local))
				}
			}
			if err == nil {
				h := sha256.Sum256(b)
				p = p + "?h=" + fmt.Sprintf("%x", h[:10])
			}
		}
		if absolute && baseURL != "" {
			return value.FromSafeString(baseURL + p), nil
		}
		return value.FromSafeString(p), nil
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
		kindSlug := cmp.Or(slug.Normalize(kind), "item")
		termSlug := cmp.Or(slug.Normalize(term), "item")
		return value.FromString("/" + kindSlug + "/" + termSlug + "/"), nil
	})

	env.AddFunction("load_data", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		p, err := firstPathArg(args, kwargs)
		if err != nil {
			return value.Undefined(), fmt.Errorf("load_data: %w", err)
		}
		normalized := strings.ReplaceAll(p, "\\", "/")
		resolved := strings.TrimPrefix(path.Clean(normalized), "/")
		if strings.HasPrefix(normalized, "./") || strings.HasPrefix(normalized, "../") {
			if currentPath, ok := currentContentPath(state); ok {
				resolved = path.Join("content", path.Dir(currentPath), resolved)
			}
		}
		b, err := fs.ReadFile(sourceFS, resolved)
		if err != nil {
			return value.Undefined(), err
		}

		format := strings.TrimPrefix(strings.ToLower(path.Ext(resolved)), ".")
		if fmtVal, ok := kwargs["format"]; ok {
			if s, ok := fmtVal.AsString(); ok {
				format = s
			}
		}

		return parseData(b, format)
	})

	urlCache := &responseCache{}

	env.AddFunction("load_url", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		rawURL, ok := kwargs["url"]
		if !ok {
			return value.Undefined(), fmt.Errorf("load_url requires a url kwarg")
		}
		urlStr, ok := rawURL.AsString()
		if !ok {
			return value.Undefined(), fmt.Errorf("load_url url must be a string")
		}

		method := http.MethodGet
		if v, ok := kwargs["method"]; ok {
			if s, ok := v.AsString(); ok {
				method = strings.ToUpper(s)
			}
		}

		bodyStr := ""
		if v, ok := kwargs["body"]; ok {
			if s, ok := v.AsString(); ok {
				bodyStr = s
			}
		}

		// format kwarg is caller-side; resolve it before cache lookup
		format := ""
		if fmtVal, ok := kwargs["format"]; ok {
			if s, ok := fmtVal.AsString(); ok {
				format = s
			}
		}

		var bodyReader io.Reader
		if bodyStr != "" {
			bodyReader = strings.NewReader(bodyStr)
		}

		req, err := http.NewRequest(method, urlStr, bodyReader)
		if err != nil {
			return value.Undefined(), fmt.Errorf("load_url: invalid request: %w", err)
		}
		req.Header.Set("User-Agent", "kopkop")

		if v, ok := kwargs["headers"]; ok {
			if hdrs, ok := v.AsSlice(); ok {
				for _, hdr := range hdrs {
					s, ok := hdr.AsString()
					if !ok {
						continue
					}
					k, v, found := strings.Cut(s, ":")
					if !found {
						continue
					}
					req.Header.Set(strings.TrimSpace(k), strings.TrimSpace(v))
				}
			}
		}

		entry, err := urlCache.Load(req, bodyStr)
		if err != nil {
			return value.Undefined(), err
		}
		return parseData(entry.body, urlFormat(format, req.URL, entry.contentType))
	})

	env.AddFunction("get_hash", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
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

		rawPath := strings.TrimPrefix(path.Clean(strings.ReplaceAll(raw, "\\", "/")), "/")
		if b, err := fs.ReadFile(sourceFS, rawPath); err == nil {
			data = b
		} else if b, err := fs.ReadFile(sourceFS, path.Join("static", strings.TrimPrefix(rawPath, "static/"))); err == nil {
			data = b
		}
		sum := sha512.Sum384(data)
		if base64Out {
			return value.FromString(base64.StdEncoding.EncodeToString(sum[:])), nil
		}
		return value.FromString(fmt.Sprintf("%x", sum[:])), nil
	})

	env.AddFunction("get_image_metadata", func(state *minijinja.State, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		p, err := firstPathArg(args, kwargs)
		if err != nil {
			return value.Undefined(), fmt.Errorf("get_image_metadata: %w", err)
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
		// Accepts kopkop's keywords (path, width, height, op) as well as positional path, width, height.
		p, err := firstPathArg(args, kwargs)
		if err != nil {
			return value.Undefined(), fmt.Errorf("resize_image: %w", err)
		}
		intArg := func(name string, pos int) (int, error) {
			v, ok := kwargs[name]
			if !ok && len(args) > pos {
				v, ok = args[pos], true
			}
			if !ok {
				return 0, nil
			}
			n, isInt := v.AsInt()
			if !isInt {
				return 0, fmt.Errorf("resize_image %s must be an integer", name)
			}
			return int(n), nil
		}
		w, err := intArg("width", 1)
		if err != nil {
			return value.Undefined(), err
		}
		h, err := intArg("height", 2)
		if err != nil {
			return value.Undefined(), err
		}
		op := imageproc.OpFill
		if v, ok := kwargs["op"]; ok {
			if op, ok = v.AsString(); !ok {
				return value.Undefined(), fmt.Errorf("resize_image op must be a string")
			}
		}
		res, err := img.Process(p, op, w, h)
		if err != nil {
			return value.Undefined(), err
		}
		return value.FromMap(map[string]value.Value{
			"url":         value.FromString(configBaseURL(state) + res.URL),
			"static_path": value.FromString(res.StaticPath),
			"width":       value.FromInt(int64(res.Width)),
			"height":      value.FromInt(int64(res.Height)),
		}), nil
	})

	// Tera spells the escaped slash &#x2F; where MiniJinja uses &#x2f;.
	teraEscape := func(state minijinja.FilterState, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		if val.IsNone() || val.IsUndefined() {
			return value.FromSafeString(""), nil
		}
		return value.FromSafeString(teraHTMLEscaper.Replace(val.String())), nil
	}
	env.AddFilter("escape", teraEscape)
	env.AddFilter("e", teraEscape)

	env.AddFilter("base64_encode", func(state minijinja.FilterState, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		s, ok := val.AsString()
		if !ok {
			return value.Undefined(), fmt.Errorf("base64_encode expects string")
		}
		return value.FromString(base64.StdEncoding.EncodeToString([]byte(s))), nil
	})

	env.AddFilter("base64_decode", func(state minijinja.FilterState, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
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

	env.AddFilter("concat", func(state minijinja.FilterState, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		other, ok := kwargs["with"]
		if !ok && len(args) > 0 {
			other = args[0]
			ok = true
		}
		if !ok {
			return value.Undefined(), fmt.Errorf("concat expects with=...")
		}

		left, lOK := val.AsSlice()
		if !lOK {
			if val.IsUndefined() || val.IsNone() {
				left = []value.Value{}
				lOK = true
			}
		}
		right, rOK := other.AsSlice()
		if !rOK {
			if other.IsUndefined() || other.IsNone() {
				right = []value.Value{}
				rOK = true
			}
		}
		if !lOK || !rOK {
			return value.Undefined(), fmt.Errorf("concat expects two arrays")
		}
		out := make([]value.Value, 0, len(left)+len(right))
		out = append(out, left...)
		out = append(out, right...)
		return value.FromSlice(out), nil
	})

	env.AddFilter("date", func(state minijinja.FilterState, val value.Value, args []value.Value, kwargs map[string]value.Value) (value.Value, error) {
		format := "%Y-%m-%d"
		if v, ok := kwargs["format"]; ok {
			if s, ok := v.AsString(); ok && s != "" {
				format = s
			}
		} else if len(args) > 0 {
			if s, ok := args[0].AsString(); ok && s != "" {
				format = s
			}
		}

		tm, ok := parseTemplateTimeValue(val)
		if !ok {
			return value.Undefined(), fmt.Errorf("date filter expects a date/time value")
		}
		if v, ok := kwargs["timezone"]; ok {
			name, _ := v.AsString()
			loc, err := time.LoadLocation(name)
			if err != nil {
				return value.Undefined(), fmt.Errorf("date filter: unknown timezone %q", name)
			}
			tm = tm.In(loc)
		}
		return value.FromString(strftime(tm, format)), nil
	})
}

func parseTemplateTimeValue(v value.Value) (time.Time, bool) {
	if raw := v.Raw(); raw != nil {
		switch t := raw.(type) {
		case time.Time:
			return t, true
		case *time.Time:
			if t != nil {
				return *t, true
			}
		}
	}
	if s, ok := v.AsString(); ok {
		layouts := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"}
		for _, layout := range layouts {
			if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// configBaseURL returns config.base_url without a trailing slash, including any
// path prefix, or "" when the template has no config.
func configBaseURL(state *minijinja.State) string {
	if cfg, ok := state.Lookup("config").AsMap(); ok {
		if bu, ok := cfg["base_url"]; ok {
			if s, ok := bu.AsString(); ok {
				return strings.TrimRight(s, "/")
			}
		}
	}
	return ""
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

func currentContentPath(state *minijinja.State) (string, bool) {
	for _, candidate := range []value.Value{
		state.Lookup("page").GetAttr("relative_path"),
		state.Lookup("section").GetAttr("relative_path"),
	} {
		if p, ok := candidate.AsString(); ok && p != "" {
			return path.Clean(strings.ReplaceAll(p, "\\", "/")), true
		}
	}
	return "", false
}
