package templates

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Not parallel: t.Setenv.
func TestLoadURLTimeout(t *testing.T) {
	for _, tc := range []struct {
		name    string
		env     string
		want    time.Duration
		wantErr string
	}{
		{name: "default", env: "", want: defaultLoadURLTimeout},
		{name: "custom", env: "5s", want: 5 * time.Second},
		{name: "garbage", env: "soon", wantErr: `invalid LOAD_URL_TIMEOUT "soon"`},
		{name: "zero", env: "0s", wantErr: "positive duration"},
		{name: "negative", env: "-1s", wantErr: "positive duration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LOAD_URL_TIMEOUT", tc.env)
			got, err := loadURLTimeout()
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
