package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeRequestPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		out  string
		ok   bool
		name string
	}{
		{name: "root", in: "/", out: "", ok: true},
		{name: "normal path", in: "/posts/one/", out: "posts/one", ok: true},
		{name: "traversal blocked", in: "/../../etc/passwd", out: "", ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := sanitizeRequestPath(tc.in)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.out, got)
		})
	}
}

func TestContentType(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "text/html; charset=utf-8", contentType("index.html"))
	assert.Equal(t, "application/json", contentType("search_index.json"))
	assert.Equal(t, "application/octet-stream", contentType("file.bin"))
}
