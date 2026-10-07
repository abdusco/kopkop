package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/abdusco/kopkop/internal/config"
	"github.com/abdusco/kopkop/internal/server"
	"github.com/abdusco/kopkop/internal/site"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "init":
		if err := runInit(os.Args[2:]); err != nil {
			log.Fatalf("init failed: %v", err)
		}
	case "build":
		if err := runBuild(os.Args[2:]); err != nil {
			log.Fatalf("build failed: %v", err)
		}
	case "serve":
		if err := runServe(os.Args[2:]); err != nil && !errors.Is(err, context.Canceled) {
			log.Fatalf("serve failed: %v", err)
		}
	case "check":
		if err := runCheck(os.Args[2:]); err != nil {
			log.Fatalf("check failed: %v", err)
		}
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("kopkop <init|build|serve|check> [flags]")
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	force := fs.Bool("force", false, "overwrite existing target")
	name := "."
	nameSet := false
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return err
		}
		args = fs.Args()
		if len(args) == 0 {
			break
		}
		if nameSet {
			return fmt.Errorf("init accepts one directory")
		}
		name, nameSet = args[0], true
		args = args[1:]
	}
	root := name
	files := []struct{ path, body string }{
		{"zola.toml", `base_url = "http://127.0.0.1:1111"
title = "My Site"
description = ""
output_dir = "public"
link_strategy = "absolute"
build_search_index = false
generate_sitemap = true
generate_feeds = false
generate_robots_txt = true
`},
		{"content/_index.md", `+++
title = "Home"
+++

Welcome to your new site.
`},
		{"templates/page.html", `<html><head><link rel="stylesheet" href="{{ get_url(path='style.css') }}"></head><body><h1>{{ page.title }}</h1>{{ page.content|safe }}</body></html>`},
		{"templates/section.html", `<html><head><link rel="stylesheet" href="{{ get_url(path='style.css') }}"></head><body><h1>{{ section.title }}</h1><ul>{% for p in section.pages %}<li><a href="{{ p.permalink }}">{{ p.title }}</a></li>{% endfor %}</ul></body></html>`},
		{"templates/index.html", `<html><head><link rel="stylesheet" href="{{ get_url(path='style.css') }}"></head><body><h1>{{ config.title }}</h1></body></html>`},
		{"static/.keep", ""},
		{"static/style.css", "body { font-family: sans-serif; margin: 2rem; line-height: 1.5; }\n" +
			"h1 { margin-bottom: 1rem; }\n" +
			"a { color: #0f4c81; text-decoration: none; }\n" +
			"a:hover { text-decoration: underline; }\n"},
	}
	// Check every destination and parent before writing the first scaffold file.
	for _, file := range files {
		p := filepath.Join(root, file.path)
		info, err := os.Lstat(p)
		if err == nil {
			if !*force || !info.Mode().IsRegular() {
				return fmt.Errorf("scaffold destination %q exists (regular files require --force)", p)
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect scaffold destination %q: %w", p, err)
		}
		for parent := filepath.Dir(p); ; parent = filepath.Dir(parent) {
			info, err := os.Stat(parent)
			if err == nil {
				if !info.IsDir() {
					return fmt.Errorf("scaffold parent %q is not a directory", parent)
				}
				break
			}
			if !os.IsNotExist(err) {
				return fmt.Errorf("inspect scaffold parent %q: %w", parent, err)
			}
			if filepath.Dir(parent) == parent {
				break
			}
		}
	}
	for _, file := range files {
		p := filepath.Join(root, file.path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return fmt.Errorf("create scaffold directory for %q: %w", p, err)
		}
		flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
		if *force {
			flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		}
		f, err := os.OpenFile(p, flags, 0o644)
		if err != nil {
			return fmt.Errorf("create scaffold file %q: %w", p, err)
		}
		_, writeErr := f.WriteString(file.body)
		closeErr := f.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			return fmt.Errorf("write scaffold file %q: %w", p, err)
		}
	}
	log.Printf("initialized site in %s", root)
	return nil
}

func runBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	root := fs.String("root", ".", "root site directory")
	configArg := fs.String("config", "", "config file name")
	baseURL := fs.String("base-url", "", "override base url")
	out := fs.String("output-dir", "", "output directory")
	_ = fs.Bool("force", false, "deprecated: the output directory is always replaced")
	drafts := fs.Bool("drafts", false, "include drafts")
	minify := fs.Bool("minify", false, "minify html")
	concurrency := fs.Int("concurrency", 0, "page rendering workers (0: GOMAXPROCS)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rootAbs, _ := filepath.Abs(*root)
	rootDir, cfgPath, err := config.DiscoverConfigPath(rootAbs, *configArg)
	if err != nil {
		return err
	}
	s, err := site.New(site.SiteParams{BasePath: rootDir, ConfigPath: cfgPath, OutputDir: *out})
	if err != nil {
		return err
	}
	if err := s.Load(*drafts); err != nil {
		return err
	}
	return s.Build(site.BuildOptions{
		IncludeDrafts: *drafts,
		BaseURL:       *baseURL,
		BuildMode:     site.BuildDisk,
		Minify:        *minify,
		Concurrency:   *concurrency,
	})
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	root := fs.String("root", ".", "root site directory")
	configArg := fs.String("config", "", "config file name")
	interfaceIP := fs.String("interface", "127.0.0.1", "bind interface")
	port := fs.Int("port", 1111, "bind port")
	baseURL := fs.String("base-url", "", "advertised preview URL (default: bound address and configured subpath)")
	drafts := fs.Bool("drafts", false, "include drafts")
	storeHTML := fs.Bool("store-html", false, "store generated HTML in memory and disk (default: memory only)")
	open := fs.Bool("open", false, "open browser")
	debounce := fs.Duration("debounce", 200*time.Millisecond, "watch debounce")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rootAbs, _ := filepath.Abs(*root)
	rootDir, cfgPath, err := config.DiscoverConfigPath(rootAbs, *configArg)
	if err != nil {
		return err
	}
	s, err := site.New(site.SiteParams{BasePath: rootDir, ConfigPath: cfgPath})
	if err != nil {
		return err
	}
	if err := s.Load(*drafts); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return server.Run(ctx, s, server.ServeOptions{
		Interface:     *interfaceIP,
		Port:          *port,
		BaseURL:       *baseURL,
		IncludeDrafts: *drafts,
		OpenBrowser:   *open,
		StoreHTML:     *storeHTML,
		Debounce:      *debounce,
	})
}

func runCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	root := fs.String("root", ".", "root site directory")
	configArg := fs.String("config", "", "config file name")
	drafts := fs.Bool("drafts", false, "include drafts")
	refresh := fs.Bool("refresh-links", false, "recheck external links ignoring cached results")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rootAbs, _ := filepath.Abs(*root)
	rootDir, cfgPath, err := config.DiscoverConfigPath(rootAbs, *configArg)
	if err != nil {
		return err
	}
	s, err := site.New(site.SiteParams{BasePath: rootDir, ConfigPath: cfgPath})
	if err != nil {
		return err
	}
	if err := s.Load(*drafts); err != nil {
		return err
	}
	if err := s.Build(site.BuildOptions{IncludeDrafts: *drafts, BuildMode: site.BuildDisk}); err != nil {
		return err
	}
	s.Config.LinkChecker.Refresh = *refresh
	results, err := s.CheckLinks()
	if err != nil {
		return err
	}
	failed := 0
	warnings := 0
	for _, r := range results {
		if !r.OK {
			level := s.Config.LinkChecker.ExternalLevel
			if r.Internal {
				level = s.Config.LinkChecker.InternalLevel
			}
			warnOnly := level == config.LinkCheckerWarn
			if warnOnly {
				warnings++
				log.Printf("[WARN] %s (%s): %s", r.URL, r.Source, r.Error)
			} else {
				failed++
				log.Printf("[BAD] %s (%s): %s", r.URL, r.Source, r.Error)
			}
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d broken links", failed)
	}
	if warnings == 0 {
		log.Printf("all links OK (%d checked)", len(results))
	} else {
		log.Printf("link check warnings: %d broken links (%d checked)", warnings, len(results))
	}
	return nil
}
