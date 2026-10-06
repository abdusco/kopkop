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
		{in: " Crème Brûlée ", want: "crème-brûlée"},
		{in: "Cafe\u0301", want: "café"},
		{in: "你好 世界", want: "你好-世界"},
		{in: "Привет Мир", want: "привет-мир"},
		{in: "नमस्ते दुनिया", want: "नमस्ते-दुनिया"},
		{in: "مرحبا بالعالم", want: "مرحبا-بالعالم"},
		{in: "🔥 !!!", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, Normalize(tc.in))
		})
	}
}
