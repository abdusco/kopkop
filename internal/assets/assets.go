package assets

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var whitespaceRe = regexp.MustCompile(`\s+`)

func CleanOutput(path string) error {
	if _, err := os.Stat(path); err == nil {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return os.MkdirAll(path, 0o755)
}

func CopyDirectory(src string, dst string) error {
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src string, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

func CompileSass(basePath string, outputPath string) error {
	return CompileSassDir(filepath.Join(basePath, "sass"), outputPath)
}

func CompileSassDir(sassDir string, outputPath string) error {
	if _, err := os.Stat(sassDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	_, sassErr := exec.LookPath("sass")

	return filepath.WalkDir(sassDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".scss" && ext != ".sass" {
			return nil
		}
		if strings.HasPrefix(name, "_") {
			return nil
		}

		rel, relErr := filepath.Rel(sassDir, path)
		if relErr != nil {
			return relErr
		}
		dstRel := strings.TrimSuffix(rel, ext) + ".css"
		dst := filepath.Join(outputPath, dstRel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}

		if sassErr == nil {
			cmd := exec.Command("sass", "--no-source-map", path, dst)
			if output, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
				return fmt.Errorf("sass compile %s: %w: %s", rel, cmdErr, strings.TrimSpace(string(output)))
			}
			return nil
		}

		compiled, compileErr := compileSassFallback(path, sassDir)
		if compileErr != nil {
			return compileErr
		}
		if err := os.WriteFile(dst, []byte(compiled), 0o644); err != nil {
			return err
		}
		return nil
	})
}

func compileSassFallback(path string, sassRoot string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".scss" {
		s := resolveScssImports(string(b), filepath.Dir(path), sassRoot)
		return flattenScss(s), nil
	}
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" {
		return "", nil
	}
	return strings.ReplaceAll(trimmed, "\n", " "), nil
}

func resolveScssImports(src string, dir string, sassRoot string) string {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "@import ") && strings.HasSuffix(trim, ";") {
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trim, "@import "), ";"))
			name = strings.Trim(name, `"'`)
			candidates := []string{
				filepath.Join(dir, "_"+name+".scss"),
				filepath.Join(dir, name+".scss"),
				filepath.Join(sassRoot, "_"+name+".scss"),
				filepath.Join(sassRoot, name+".scss"),
			}
			for _, c := range candidates {
				if b, err := os.ReadFile(c); err == nil {
					out = append(out, resolveScssImports(string(b), filepath.Dir(c), sassRoot))
					goto nextLine
				}
			}
		}
		out = append(out, line)
	nextLine:
	}
	return strings.Join(out, "\n")
}

func flattenScss(src string) string {
	stack := []string{}
	rules := []string{}
	token := strings.Builder{}

	emitDecl := func(selector string, decl string) {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			return
		}
		parts := strings.SplitN(decl, ":", 2)
		if len(parts) != 2 {
			return
		}
		left := strings.TrimSpace(parts[0])
		right := strings.TrimSpace(parts[1])
		selector = strings.TrimSpace(selector)
		if selector == "" {
			return
		}
		rules = append(rules, selector+"{"+left+":"+right+"}")
	}

	for _, r := range src {
		switch r {
		case '{':
			sel := strings.TrimSpace(token.String())
			token.Reset()
			parent := ""
			if len(stack) > 0 {
				parent = strings.TrimSpace(stack[len(stack)-1])
			}
			if parent != "" {
				sel = strings.TrimSpace(parent + " " + sel)
			}
			stack = append(stack, sel)
		case ';':
			if len(stack) > 0 {
				emitDecl(stack[len(stack)-1], token.String())
			}
			token.Reset()
		case '}':
			token.Reset()
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		default:
			token.WriteRune(r)
		}
	}

	out := strings.Join(rules, "")
	out = whitespaceRe.ReplaceAllString(out, " ")
	out = strings.ReplaceAll(out, "{ ", "{")
	out = strings.ReplaceAll(out, " }", "}")
	out = strings.ReplaceAll(out, "; ", ";")
	out = strings.ReplaceAll(out, " :", ":")
	out = strings.ReplaceAll(out, ": ", ":")
	return strings.TrimSpace(out)
}
