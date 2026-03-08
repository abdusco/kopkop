package site

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkSiteBuild(b *testing.B) {
	root := b.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "content"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "templates"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "zola.toml"), []byte(`
base_url = "https://example.com"
title = "Bench"
output_dir = "public"
generate_sitemap = true
generate_feeds = false
build_search_index = false
generate_robots_txt = true
`), 0o644)
	_ = os.WriteFile(filepath.Join(root, "content", "_index.md"), []byte("+++\ntitle='Home'\n+++\nHome"), 0o644)
	for i := 0; i < 50; i++ {
		name := filepath.Join(root, "content", "post-"+itoa(i)+".md")
		_ = os.WriteFile(name, []byte("+++\ntitle='Post'\n+++\nBody"), 0o644)
	}
	_ = os.WriteFile(filepath.Join(root, "templates", "page.html"), []byte("<html><body>{{ page.content|safe }}</body></html>"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "templates", "section.html"), []byte("<html><body>{{ section.title }}</body></html>"), 0o644)

	s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "zola.toml")})
	if err != nil {
		b.Fatal(err)
	}
	if err := s.Load(false); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.Build(BuildOptions{BuildMode: BuildDisk, Force: true}); err != nil {
			b.Fatal(err)
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	v := i
	for v > 0 {
		pos--
		buf[pos] = byte('0' + (v % 10))
		v /= 10
	}
	return string(buf[pos:])
}
