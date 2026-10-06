package pathing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputePageSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		metaSlug        string
		filePathForSlug string
		keepDates       bool
		wantSlug        string
		wantDate        string
	}{
		{
			name:            "default slug from filename",
			filePathForSlug: "My First Post",
			wantSlug:        "my-first-post",
		},
		{name: "empty slug has no fallback", filePathForSlug: "🔥", wantSlug: ""},
		{name: "date-like prefix requires separator", filePathForSlug: "2024-01-01garbage", wantSlug: "2024-01-01garbage"},
		{name: "slug override keeps filename date", metaSlug: "Custom", filePathForSlug: "2024-01-01-post", wantSlug: "custom", wantDate: "2024-01-01"},
		{
			name:            "slug override wins",
			metaSlug:        "Hello World",
			filePathForSlug: "ignored-name",
			wantSlug:        "hello-world",
		},
		{
			name:            "dated filename strips date when keep dates disabled",
			filePathForSlug: "2002-10-02T15:00:00Z-my-post",
			keepDates:       false,
			wantSlug:        "my-post",
			wantDate:        "2002-10-02T15:00:00Z",
		},
		{
			name:            "dated filename keeps full filename when keep dates enabled",
			filePathForSlug: "2002-10-02T15:00:00Z-my-post",
			keepDates:       true,
			wantSlug:        "2002-10-02t15-00-00z-my-post",
			wantDate:        "2002-10-02T15:00:00Z",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			slug, date := ComputePageSlug(tc.metaSlug, tc.filePathForSlug, tc.keepDates)
			assert.Equal(t, tc.wantSlug, slug)
			assert.Equal(t, tc.wantDate, date)
		})
	}
}

func TestComputePagePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		metaPath         string
		slug             string
		components       []string
		fileName         string
		hasColocatedPath bool
		want             string
	}{
		{
			name:     "path override is normalized",
			metaPath: "posts/custom",
			want:     "/posts/custom/",
		},
		{
			name:             "root index without colocated path",
			fileName:         "index",
			hasColocatedPath: false,
			want:             "/",
		},
		{
			name:       "page path from components and slug",
			slug:       "my-post",
			components: []string{"blog", "2026"},
			fileName:   "post",
			want:       "/blog/2026/my-post/",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ComputePagePath(tc.metaPath, tc.slug, tc.components, tc.fileName, tc.hasColocatedPath)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMakePermalink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		path    string
		want    string
	}{
		{
			name:    "joins base and page path",
			baseURL: "https://example.com",
			path:    "/blog/my-post/",
			want:    "https://example.com/blog/my-post/",
		},
		{
			name:    "trims trailing slash from base",
			baseURL: "https://example.com/",
			path:    "/docs/",
			want:    "https://example.com/docs/",
		},
		{
			name:    "normalizes missing leading slash in path",
			baseURL: "https://example.com",
			path:    "tags/rust/",
			want:    "https://example.com/tags/rust/",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := MakePermalink(tc.baseURL, tc.path)
			assert.Equal(t, tc.want, got)
		})
	}
}
