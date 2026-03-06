# kopkop

A Go-based static site generator behaviorally compatible with [Zola](https://www.getzola.org/). Existing Zola sites can build with minimal changes while preserving Zola semantics.

## Installation

```sh
go install github.com/abdusco/kopkop/cmd/kopkop@latest
```

Or build from source:

```sh
git clone https://github.com/abdusco/kopkop
cd kopkop
go build ./cmd/kopkop
```

Requires Go 1.22+.

## Quick Start

```sh
# Create a new site
kopkop init mysite
cd mysite

# Build the site to ./public
kopkop build

# Start the dev server with live reload
kopkop serve

# Check external links
kopkop check
```

## Commands

### `init [dir]`

Scaffolds a new site with a minimal directory structure:

```
mysite/
  zola.toml         # site config
  content/
    _index.md       # root section
  templates/
    index.html
    page.html
    section.html
  static/
    style.css
```

**Flags:**
- `--force` — overwrite existing files

### `build`

Renders the full site to the output directory.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--root` | `.` | Root site directory |
| `--config` | auto-discovered | Config file name |
| `--base-url` | from config | Override base URL |
| `--output-dir` | from config | Output directory |
| `--drafts` | `false` | Include draft pages |
| `--minify` | `false` | Minify HTML output |
| `--force` | `false` | Overwrite existing output directory |

**Build order:**
1. Clean output directory (disk mode only)
2. Render pages (markdown + shortcodes + templates)
3. Render alias redirects
4. Render sections
5. Render taxonomies
6. Render sitemap (if `generate_sitemap`)
7. Render feeds (if `generate_feeds`)
8. Render 404
9. Render robots.txt (if `generate_robots_txt`)
10. Build search index (if `build_search_index` or `search.build_index`)
11. Copy static directory
12. Copy colocated assets

### `serve`

Starts a local dev server with filesystem watching and WebSocket live reload.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--root` | `.` | Root site directory |
| `--interface` | `127.0.0.1` | Bind interface |
| `--port` | `1111` | Bind port |
| `--drafts` | `false` | Include drafts |
| `--open` | `false` | Open browser on start |
| `--debounce` | `200ms` | File watcher debounce duration |
| `--store-html` | `false` | Also write HTML to disk (default: memory only) |
| `--fast` | `false` | Fast rebuild mode |

By default, generated HTML is served from memory without writing to disk. The server watches `content/`, `templates/`, `static/`, and the config file for changes. Extra paths can be added via `extra_watch_paths` in the config.

Live reload is injected automatically via a WebSocket endpoint at `/__livereload`.

### `check`

Builds the site and then verifies all external links found in the rendered content.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--root` | `.` | Root site directory |
| `--drafts` | `false` | Include drafts |

Exit code is non-zero if broken links are found (unless `link_checker.internal_level = "warn"`).

## Configuration

Config is loaded from `zola.toml` or `config.toml`, searched in the current directory and its ancestors.

```toml
base_url = "https://example.com"
title = "My Site"
description = "A site built with kopkop"
author = "Alice"
output_dir = "public"

# "absolute" or "relative"
link_strategy = "absolute"

# Theme to use (from themes/<name>/)
theme = ""

build_search_index = false
generate_sitemap = true
generate_feeds = false
feed_filenames = ["atom.xml"]
generate_robots_txt = true
minify_html = false

# Glob patterns for content files to ignore
ignored_content = []

# Additional paths to watch in serve mode
extra_watch_paths = []

# Whether to preserve file modification dates on output files
paths_keep_dates = false

# Include drafts in all builds (overridden by --drafts flag)
enable_drafts_in_build = false

[markdown]
insert_anchor_links = false
external_links_target_blank = false
highlight_code = false

[search]
build_index = false
index_path = "search_index.json"

[link_checker]
internal_level = "error"   # "warn" or "error"
timeout_seconds = 10
use_cache = true
cache_file = ".kopkop-linkcheck-cache.json"
skip_anchor_prefixes = []

[[taxonomies]]
name = "tags"
feed = false

[extra]
# Arbitrary key-value pairs accessible in templates as config.extra
```

### Theme Config Merge

If `theme` is set, kopkop loads `themes/<theme>/theme.toml` and merges values that are not set in the main config (title, description, taxonomies, extra).

## Content

### Directory Layout

```
content/
  _index.md           # root section
  blog/
    _index.md         # blog section
    first-post.md     # page at /blog/first-post/
    second-post/
      index.md        # page with colocated assets
      image.png       # colocated asset
```

### Front Matter

Pages and sections support TOML (`+++`) or YAML (`---`) front matter.

**Page front matter:**

```toml
+++
title = "My Post"
description = "A short description"
date = 2024-01-15
updated = 2024-02-01
slug = "custom-slug"       # overrides URL slug
path = "/custom/path/"     # full path override
weight = 10
draft = false
render = true
template = "custom.html"   # override template
aliases = ["/old/url/"]
insert_anchor_links = "left"

[taxonomies]
tags = ["go", "web"]

[extra]
custom_field = "value"
+++

Page content goes here.
```

**Section front matter (`_index.md`):**

```toml
+++
title = "Blog"
description = "All posts"
template = "blog.html"
page_template = "post.html"
sort_by = "date"         # "date", "weight", or anything else (preserves loader order)
paginate_by = 10
paginate_path = "page"
paginate_reversed = false
generate_feed = false
transparent = false
draft = false
weight = 0
+++
```

### Date Extraction

Dates can be embedded in filenames: `2024-01-15-my-post.md` sets the page date automatically.

### Summary

Insert `<!-- more -->` in content to define where the summary ends:

```markdown
This is the summary shown in listings.

<!-- more -->

This content is only shown on the full page.
```

## Templates

Templates use [MiniJinja](https://github.com/mitsuhiko/minijinja) syntax (compatible with Jinja2/Tera). Templates live in `templates/` and theme templates in `themes/<name>/templates/`. Site templates take precedence over theme templates.

### Template Resolution

Resolution happens in two stages.

**Stage 1 — choose a template name** based on content type:

| Content type | Template name selection |
|---|---|
| Page | `template` front matter → parent section's `page_template` (walked up to root) → `page.html` |
| Section | `template` front matter → `index.html` (root section only, if the file exists) → `section.html` |
| Taxonomy list | `<name>/list.html` → `<slug>/list.html` → `taxonomy_list.html` |
| Taxonomy term | `<name>/single.html` → `<slug>/single.html` → `taxonomy_single.html` |
| 404 | `404.html` |
| Sitemap | `sitemap.xml` |
| Feed | feed filename (e.g. `atom.xml`) |
| Robots | `robots.txt` |

`→` means "fall back to the next if missing or not set".

**Stage 2 — find the template file** for the chosen name:

1. `templates/<name>` — site templates (highest priority)
2. `themes/<theme>/templates/<name>` — theme templates
3. Built-in fallback (covers 404, sitemap, atom/RSS feeds, robots.txt, alias redirects)

Site templates always override theme templates. The built-ins only apply if neither the site nor the theme provides the template.

### Template Variables

**Page context:**
```jinja
{{ page.title }}
{{ page.description }}
{{ page.content }}        {# rendered HTML #}
{{ page.summary }}        {# HTML up to <!-- more --> #}
{{ page.permalink }}
{{ page.path }}
{{ page.slug }}
{{ page.date }}
{{ page.updated }}
{{ page.draft }}
{{ page.toc }}            {# list of {id, level, title} #}
{{ page.assets }}         {# colocated asset URLs #}
{{ page.taxonomies }}     {# map of taxonomy name to list of terms #}
{{ page.extra }}
{{ config.title }}
{{ config.base_url }}
{{ config.extra }}
```

**Section context:**
```jinja
{{ section.title }}
{{ section.content }}
{{ section.permalink }}
{{ section.pages }}       {# list of page objects #}
{{ section.subsections }} {# list of subsection relative paths (strings) #}
{{ paginator }}           {# pagination object, present when paginate_by > 0 #}
```

When `paginate_by` is set, `paginator` contains: `current_index`, `number_pagers`, `per_page`, `total_pages`, `first`, `last`, `current`, `pages`, and optionally `previous` / `next`.

### Template Functions

| Function | Description |
|----------|-------------|
| `get_url(path=...)` | Resolve a URL. Supports `cachebust=true` and `absolute=true/false`. |
| `get_page(path=...)` | Look up a page by its content path (e.g. `"blog/post.md"`). |
| `get_section(path=...)` | Look up a section by its content path (e.g. `"blog/_index.md"`). |
| `get_taxonomy(kind=...)` | Get a full taxonomy object by name. |
| `get_taxonomy_term(kind=..., term=...)` | Get a single taxonomy term with its pages. |
| `get_taxonomy_url(kind=..., name=...)` | Get the URL for a taxonomy term. |
| `now()` | Current UTC time as RFC3339 string. |
| `get_env(name=..., default=...)` | Read an environment variable. |
| `load_data(path)` | Load a file relative to the site root as a string. |
| `get_hash(path, base64=false)` | SHA-384 hash of a file or string. |
| `get_image_metadata(path)` | Returns `{width, height, format}` for an image. |
| `resize_image(path, width, height)` | Resize an image and return its URL. |

### Template Filters

| Filter | Description |
|--------|-------------|
| `\| markdown` | Render a string as Markdown to HTML. |
| `\| base64_encode` | Base64-encode a string. |
| `\| base64_decode` | Base64-decode a string. |
| `\| regex_replace(pattern, replacement)` | Replace regex matches in a string. |
| `\| num_format(precision)` | Format a number with fixed decimal places (default: 2). |
| `\| default(value)` | Return `value` if the input is undefined or empty. |
| `\| concat(with=...)` | Concatenate two arrays. |
| `\| date(format=...)` | Format a date using strftime-style patterns (e.g. `"%Y-%m-%d"`). |

### Tera Compatibility

kopkop normalizes common Tera template syntax to MiniJinja on load:
- Named end tags: `{% endmacro name %}` becomes `{% endmacro %}`
- Macro call syntax: `macros::func(...)` becomes `macros.func(...)`

## Shortcodes

Shortcodes are templates placed in `templates/shortcodes/`. They can be `.html` (rendered after Markdown) or `.md` (rendered before, so Markdown inside is processed).

### Inline Shortcode

```
{{ youtube(id="dQw4w9WgXcQ") }}
```

Template at `templates/shortcodes/youtube.html`:
```html
<iframe src="https://www.youtube.com/embed/{{ id }}"></iframe>
```

### Body Shortcode

```
{% quote(author="Jane Doe") %}
This is the body content passed to the shortcode.
{% end %}
```

Template at `templates/shortcodes/quote.html`:
```html
<blockquote>
  {{ body }}
  <cite>— {{ author }}</cite>
</blockquote>
```

### Ignored Shortcodes

To show a shortcode as literal text without rendering it, wrap with comment markers:

```
{{/* youtube(id="example") */}}
{%/* quote(author="x") */%} body {%/* end */%}
```

## Markdown Features

- **Heading anchors** — set `insert_anchor_links = true` in `[markdown]` to add `#` anchor links to headings
- **Syntax highlighting** — set `highlight_code = true` to use Chroma-based highlighting with the GitHub theme; CSS classes are prefixed with `z-`
- **External link behavior** — set `external_links_target_blank = true` to add `target="_blank" rel="noopener"` to external links
- **Internal links** — use `@/path/to/page.md` syntax to link to content by path; broken links cause a build error
- **Colocated assets** — relative links in content resolve against the page's permalink

## Search Index

When `build_search_index = true`, a JSON file is written to `search.index_path` (default: `search_index.json`) containing:

```json
[
  {
    "title": "Page Title",
    "permalink": "https://example.com/blog/post/",
    "summary": "...",
    "content": "plain text content stripped of HTML tags"
  }
]
```

## Feeds

When `generate_feeds = true`, feeds are generated for the site and any sections with `generate_feed = true`. Feed templates default to Atom (`atom.xml`) but can be configured via `feed_filenames`.

## Static Assets

Files in `static/` are copied verbatim to the output directory. Colocated assets (files next to a page in `content/`) are copied adjacent to the rendered page output.

## Image Processing

Images can be resized via the `resize_image` template function. Processed images are written to the output directory and served normally.

## Project Layout

```
cmd/kopkop/         CLI entry point
internal/
  config/           Config loading, validation, theme merge
  content/          Page/section parsing, front matter, taxonomy, content graph
  markdown/         Markdown rendering, shortcode parsing, link resolution
  templates/        MiniJinja engine, filters, global functions, template manager
  site/             Build orchestrator, output writing
  assets/           Static file copy, image processing
  search/           Search index generation
  server/           Dev server, filesystem watcher, WebSocket live reload
  linkcheck/        External link verification
```
