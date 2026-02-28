# Migrating from Zola to Kopkop

This document tracks practical migration notes while Kopkop reaches full parity.

## 1. Template Engine

- Kopkop uses native Go MiniJinja: `github.com/mitsuhiko/minijinja/minijinja-go/v2`.
- Core Jinja-style constructs work: inheritance, include, macros, filters/functions.
- Template fallback order is preserved:
  1. site templates
  2. theme templates (`<theme>/templates/...`)
  3. built-ins (`__zola_builtins/...`)

## 2. Config

- `zola.toml` and `config.toml` are both discoverable.
- Theme merge via `themes/<theme>/theme.toml` is supported for selected fields.

## 3. Content

- TOML and YAML front matter are supported.
- Pages and sections (`_index.md`) are loaded into a library graph.
- Slugs, paths, permalinks, taxonomies, and draft filtering are supported.

## 4. Markdown and Shortcodes

- Markdown rendering includes TOC, heading anchors, summary divider, and link rewriting.
- Shortcode parsing and insertion pipeline is implemented for both md/html shortcode phases.

## 5. Build/Serve/Check Commands

- `kopkop build`: full static build pipeline.
- `kopkop serve`: file watch + rebuild + websocket live reload.
- `kopkop check`: external link checks with optional anchor validation.

## 6. Parity Testing

- Unit and integration tests run with `go test ./...`.
- Optional differential mode compares output against Zola:
  - `PARITY_RUN_DIFF=1`
  - `PARITY_ZOLA_BIN=/path/to/zola`

## 7. Current Compatibility Notes

Kopkop is feature-rich but not yet a perfect output-identical Zola replacement.
Use `tests/parity/known_diffs.yaml` to record and burn down intentional differences.
