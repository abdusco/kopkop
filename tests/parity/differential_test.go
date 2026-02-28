package parity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/abdusco/kopkop/tests/harness"
)

type knownDiff struct {
	Fixture string `yaml:"fixture"`
	Pattern string `yaml:"pattern"`
}

func TestGoVsZolaDifferential_Optional(t *testing.T) {
	t.Parallel()

	if os.Getenv("PARITY_RUN_DIFF") != "1" {
		t.Skip("set PARITY_RUN_DIFF=1 to run differential parity tests")
	}
	zolaBin := os.Getenv("PARITY_ZOLA_BIN")
	if zolaBin == "" {
		t.Skip("set PARITY_ZOLA_BIN to zola binary path")
	}

	fixtures := []string{"test_site", "test_site_i18n"}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture, func(t *testing.T) {
			root := filepath.Join("..", "fixtures", "zola", fixture)
			cfgName := "zola.toml"
			if _, err := os.Stat(filepath.Join(root, cfgName)); err != nil {
				cfgName = "config.toml"
			}

			goOut := filepath.Join(t.TempDir(), "go-public")
			zolaOut := filepath.Join(t.TempDir(), "zola-public")

			require.NoError(t, harness.BuildWithKopkop(root, filepath.Join(root, cfgName), goOut, false))
			require.NoError(t, harness.BuildWithZola(zolaBin, root, cfgName, zolaOut, false))

			ignore, err := loadKnownDiffIgnore(filepath.Join("known_diffs.yaml"), fixture)
			require.NoError(t, err)

			diffs, err := harness.CompareDirectories(goOut, zolaOut, ignore)
			require.NoError(t, err)
			require.Empty(t, diffs)
		})
	}
}

func loadKnownDiffIgnore(path string, fixture string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var items []knownDiff
	if err := yaml.Unmarshal(b, &items); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it.Pattern != "" && (it.Fixture == "" || it.Fixture == fixture) {
			out = append(out, it.Pattern)
		}
	}
	return out, nil
}
