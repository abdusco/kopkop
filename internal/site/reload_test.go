package site

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReloadPreservesConstructionOverrides(t *testing.T) {
	t.Parallel()
	for _, override := range []string{"", "custom-output"} {
		t.Run(override, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			cfg := filepath.Join(root, "config.toml")
			require.NoError(t, os.WriteFile(cfg, []byte("base_url='https://example.com'\noutput_dir='original'\n"), 0o644))
			s, err := New(SiteParams{BasePath: root, ConfigPath: cfg, OutputDir: override})
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(cfg, []byte("base_url='https://updated.example'\noutput_dir='updated'\n"), 0o644))
			next, err := s.Reload()
			require.NoError(t, err)
			require.Equal(t, "https://updated.example", next.Config.BaseURL)
			want := "updated"
			if override != "" {
				want = override
			}
			require.Equal(t, filepath.Join(root, want), next.OutputPath)
			require.Equal(t, "https://example.com", s.Config.BaseURL)
			require.NoError(t, os.WriteFile(cfg, []byte("invalid = ["), 0o644))
			_, err = s.Reload()
			require.Error(t, err)
			require.Equal(t, "https://example.com", s.Config.BaseURL)
		})
	}
}
