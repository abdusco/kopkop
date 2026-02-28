package assets

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompileSassDir_FallbackCopiesWhenSassBinaryMissingOrUnavailable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sassDir := filepath.Join(root, "sass")
	out := filepath.Join(root, "public")
	require.NoError(t, os.MkdirAll(sassDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sassDir, "site.scss"), []byte("$x: red; body { color: $x; }"), 0o644))

	require.NoError(t, CompileSassDir(sassDir, out))
	require.FileExists(t, filepath.Join(out, "site.css"))
	b, err := os.ReadFile(filepath.Join(out, "site.css"))
	require.NoError(t, err)
	require.Contains(t, []string{"body{color:$x}", "body{color:red}", "body { color: red; }"}, string(b))
}

func TestCompileSassDir_CompilesNestedEntries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sassDir := filepath.Join(root, "sass")
	out := filepath.Join(root, "public")
	require.NoError(t, os.MkdirAll(filepath.Join(sassDir, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sassDir, "nested", "site.scss"), []byte("$x: red; body { color: $x; }"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sassDir, "nested", "_partial.scss"), []byte("$x: red;"), 0o644))

	require.NoError(t, CompileSassDir(sassDir, out))
	require.FileExists(t, filepath.Join(out, "nested", "site.css"))
	require.NoFileExists(t, filepath.Join(out, "nested", "_partial.css"))
	b, err := os.ReadFile(filepath.Join(out, "nested", "site.css"))
	require.NoError(t, err)
	require.Contains(t, []string{"body{color:$x}", "body{color:red}", "body { color: red; }"}, string(b))
}

func TestCompileSassDir_FlattensNestedRulesAndImports(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sassDir := filepath.Join(root, "sass")
	out := filepath.Join(root, "public")
	require.NoError(t, os.MkdirAll(sassDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sassDir, "_included.scss"), []byte(".container {\n  font-size: 2rem;\n}"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sassDir, "blog.scss"), []byte("body {\n  background: red;\n\n  .container {\n    background: blue;\n  }\n}\n\n@import \"included\";\n"), 0o644))

	require.NoError(t, CompileSassDir(sassDir, out))
	b, err := os.ReadFile(filepath.Join(out, "blog.css"))
	require.NoError(t, err)
	require.Equal(t, "body{background:red}body .container{background:blue}.container{font-size:2rem}", string(b))
}
