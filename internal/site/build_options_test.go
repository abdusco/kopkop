package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInvalidBuildOptionsPreserveOutputAndConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name      string
		opts      BuildOptions
		wantError string
	}{
		{"relative base URL", BuildOptions{BaseURL: "/blog"}, "absolute HTTP"},
		{"negative concurrency", BuildOptions{Concurrency: -1}, "concurrency"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			configFile := filepath.Join(root, "config.toml")
			require.NoError(t, os.WriteFile(configFile, []byte("base_url='https://example.com'"), 0o644))
			s, err := New(SiteParams{BasePath: root, ConfigPath: configFile})
			require.NoError(t, err)
			require.NoError(t, os.MkdirAll(s.OutputPath, 0o755))
			sentinel := filepath.Join(s.OutputPath, "sentinel")
			require.NoError(t, os.WriteFile(sentinel, []byte("keep"), 0o644))
			require.ErrorContains(t, s.Build(tc.opts), tc.wantError)
			require.Equal(t, "https://example.com", s.Config.BaseURL)
			data, err := os.ReadFile(sentinel)
			require.NoError(t, err)
			require.Equal(t, "keep", string(data))
		})
	}
}
