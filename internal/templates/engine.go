package templates

import (
	"path"
	"strings"

	minijinja "github.com/mitsuhiko/minijinja/minijinja-go/v2"
)

type Engine struct {
	env *minijinja.Environment
}

func NewEngine() *Engine {
	env := minijinja.NewEnvironment()
	// Tera-like behavior where missing nested values in conditionals are tolerated,
	// while still surfacing many undefined issues.
	env.SetUndefinedBehavior(minijinja.UndefinedSemiStrict)
	env.SetDebug(true)

	return &Engine{env: env}
}

func (e *Engine) Env() *minijinja.Environment {
	return e.env
}

func (e *Engine) AddTemplate(name string, source string) error {
	return e.env.AddTemplate(name, source)
}

func (e *Engine) Render(name string, ctx any) (string, error) {
	tpl, err := e.env.GetTemplate(name)
	if err != nil {
		return "", err
	}
	return tpl.Render(ctx)
}

func (e *Engine) EnableRelativeTemplateResolution() {
	e.env.SetPathJoinCallback(DefaultTemplatePathJoin)
}

func DefaultTemplatePathJoin(name string, parent string) string {
	cleanName := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if strings.HasPrefix(cleanName, "/") {
		return strings.TrimPrefix(cleanName, "/")
	}
	if parent == "" {
		return cleanName
	}

	parentNorm := strings.ReplaceAll(parent, "\\", "/")
	parentDir := path.Dir(parentNorm)

	if !strings.HasPrefix(cleanName, "./") && !strings.HasPrefix(cleanName, "../") {
		if !strings.Contains(cleanName, "/") && parentDir != "." {
			return path.Clean(path.Join(parentDir, cleanName))
		}
		return cleanName
	}

	if parentDir == "." {
		return cleanName
	}

	return path.Clean(path.Join(parentDir, cleanName))
}
