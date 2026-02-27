package templates

import "fmt"

type Resolver struct {
	theme string
}

func NewResolver(theme string) Resolver {
	return Resolver{theme: theme}
}

func (r Resolver) Resolve(name string, available map[string]struct{}) (string, error) {
	if _, ok := available[name]; ok {
		return name, nil
	}

	if r.theme != "" {
		themeName := fmt.Sprintf("%s/templates/%s", r.theme, name)
		if _, ok := available[themeName]; ok {
			return themeName, nil
		}
	}

	builtinName := fmt.Sprintf("__zola_builtins/%s", name)
	if _, ok := available[builtinName]; ok {
		return builtinName, nil
	}

	return "", fmt.Errorf("template %q not found", name)
}
