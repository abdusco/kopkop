package site

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkLargeSiteBuild(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			root := b.TempDir()
			for _, dir := range []string{"content", "templates"} {
				if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
					b.Fatal(err)
				}
			}
			for name, body := range map[string]string{
				"config.toml":              "base_url='https://example.com'\n[[taxonomies]]\nname='tags'\n",
				"content/_index.md":      "+++\ntitle='Home'\nsort_by='weight'\n+++\nHome",
				"templates/page.html":    `<h1>{{ page.title }}</h1>{{ page.content|safe }}{{ section.title }}{{ get_page(path="post-0.md").title }}{{ get_section(path="_index.md").title }}{{ get_taxonomy(kind="tags").name }}`,
				"templates/section.html": `<h1>{{ section.title }}</h1>{% for page in section.pages %}{{ page.title }}{% endfor %}`,
			} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
					b.Fatal(err)
				}
			}
			for i := 0; i < count; i++ {
				body := fmt.Sprintf("+++\ntitle='Post %d'\nweight=%d\n[taxonomies]\ntags=['Go']\n+++\n**Body**", i, i)
				if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("content/post-%d.md", i)), []byte(body), 0o644); err != nil {
					b.Fatal(err)
				}
			}
			s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "config.toml")})
			if err != nil {
				b.Fatal(err)
			}
			if err := s.Load(false); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := s.Build(BuildOptions{BuildMode: BuildMemory, Concurrency: 1}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSiteBuild(b *testing.B) {
	root := b.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "content"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "templates"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "config.toml"), []byte(`
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

	s, err := New(SiteParams{BasePath: root, ConfigPath: filepath.Join(root, "config.toml")})
	if err != nil {
		b.Fatal(err)
	}
	if err := s.Load(false); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.Build(BuildOptions{BuildMode: BuildDisk}); err != nil {
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
