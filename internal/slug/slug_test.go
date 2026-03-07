package slug

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{in: " Hello World ", want: "hello-world"},
		{in: "hello___world", want: "hello-world"},
		{in: "a/b:c", want: "a-b-c"},
		{in: "text here `with code`", want: "text-here-with-code"},
		{in: "", want: ""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, Normalize(tc.in))
		})
	}
}
