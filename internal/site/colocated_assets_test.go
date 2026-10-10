package site

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCachebustColocatedAssets(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     BuildMode
		pagePath string
	}{
		{"clean disk", BuildDisk, "about"},
		{"serve memory", BuildMemory, "about"},
		{"disk and memory", BuildBoth, "about"},
		{"custom published path", BuildDisk, "renamed/about"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, body := range map[string]string{
				"config.toml":             "base_url='https://example.com'\nlink_strategy='relative'\n",
				"content/about/index.md":  "+++\npath='" + tc.pagePath + "'\n+++\n{{ get_url(path='" + tc.pagePath + "/dither.js', cachebust=true) }}",
				"content/about/dither.js": "initial script",
				"static/style.css":        "initial style",
				"templates/page.html":     `{{ page.content|safe }}|{{ get_url(path="/` + tc.pagePath + `/dither.js", cachebust=true) }}|{{ get_url(path="style.css", cachebust=true) }}|{{ get_url(path="missing.js", cachebust=true) }}`,
			} {
				require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
			}
			s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "config.toml")})
			require.NoError(t, err)
			for _, version := range []string{"initial", "updated"} {
				script := version + " script"
				style := version + " style"
				require.NoError(t, os.WriteFile(filepath.Join(root, "content/about/dither.js"), []byte(script), 0o644))
				require.NoError(t, os.WriteFile(filepath.Join(root, "static/style.css"), []byte(style), 0o644))
				require.NoError(t, s.Build(BuildOptions{BuildMode: tc.mode}))
				body, err := s.OutputFS.ReadFile(tc.pagePath + "/index.html")
				require.NoError(t, err)
				h := sha256.Sum256([]byte(script))
				assetURL := fmt.Sprintf("/%s/dither.js?h=%x", tc.pagePath, h[:10])
				require.Contains(t, string(body), "<p>"+assetURL+"</p>")
				require.Contains(t, string(body), "|"+assetURL+"|")
				h = sha256.Sum256([]byte(style))
				require.Contains(t, string(body), fmt.Sprintf("|/style.css?h=%x|/missing.js", h[:10]))
				copied, err := s.OutputFS.ReadFile(tc.pagePath + "/dither.js")
				require.NoError(t, err)
				require.Equal(t, script, string(copied))
			}
		})
	}
}

func TestColocatedAssetsAreCopiedRecursively(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for name, body := range map[string]string{
		"config.toml":                       "base_url = \"https://example.com\"\nignored_content = [\"*.psd\"]\n",
		"content/post/index.md":             "+++\ntitle = 'Post'\n+++\n",
		"content/post/cover.png":            "cover",
		"content/post/images/deep/a.png":    "deep",
		"content/post/.DS_Store":            "junk",
		"content/post/images/.hidden":       "junk",
		"content/post/source.psd":           "ignored",
		"content/post/sub/_index.md":        "+++\ntitle = 'Sub'\n+++\n",
		"content/post/sub/not-an-asset.png": "belongs to the section",
		"content/post/bundle/index.md":      "+++\ntitle = 'Nested bundle'\n+++\n",
		"content/post/bundle/own.png":       "belongs to the nested bundle",
		"templates/page.html":               "{% for a in page.assets %}{{ a }};{% endfor %}",
		"templates/section.html":            "section",
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o644))
	}

	s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "config.toml")})
	require.NoError(t, err)
	require.NoError(t, s.Load(false))
	require.NoError(t, s.Build(BuildOptions{BuildMode: BuildDisk}))

	out := filepath.Join(root, "public")
	require.FileExists(t, filepath.Join(out, "post", "cover.png"))
	require.FileExists(t, filepath.Join(out, "post", "images", "deep", "a.png"))
	require.NoFileExists(t, filepath.Join(out, "post", ".DS_Store"))
	require.NoFileExists(t, filepath.Join(out, "post", "images", ".hidden"))
	require.NoFileExists(t, filepath.Join(out, "post", "source.psd"))
	require.NoFileExists(t, filepath.Join(out, "post", "sub", "not-an-asset.png"))
	// A nested bundle keeps its own assets.
	require.FileExists(t, filepath.Join(out, "post", "bundle", "own.png"))
}
