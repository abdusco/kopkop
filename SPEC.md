# Zola-to-Go Port Specification

## 1. Objective

Build a Go-based static site generator that is behaviorally compatible with core Zola workflows, using:

- Go for implementation and runtime
- the native Go MiniJinja implementation from `https://github.com/mitsuhiko/minijinja/tree/main/minijinja-go`
- module import path: `github.com/mitsuhiko/minijinja/minijinja-go/v2`

Constraint: use the pure/native Go MiniJinja port directly (no Rust FFI bridge, no cgo wrapper around Rust MiniJinja).

The end goal is a practical migration path where existing Zola sites can build with minimal template/content changes, while preserving Zola semantics where feasible.

---

## 2. Non-Goals (for initial release)

The first implementation phase should explicitly defer:

- Full 1:1 parity for every niche template helper
- Advanced i18n edge-cases
- Perfect output-byte equivalence with Zola
- All image processing variants and optimization backends
- Every search index flavor and all check-mode diagnostics

These are added incrementally after MVP parity.

---

## 3. High-Level Architecture

Create a modular Go codebase modeled after Zola’s component boundaries:

- `cmd/` CLI entry points (`build`, `serve`, `check`, `init`)
- `internal/config/` config loading, validation, theme merge
- `internal/content/` page/section parsing, front matter, taxonomy, library graph
- `internal/markdown/` markdown rendering, shortcode pipeline, links/toc/summary
- `internal/templates/` MiniJinja environment, filters, globals, fallback resolution
- `internal/site/` orchestrates full build pipeline and output writes
- `internal/assets/` static copy, Sass integration, image processing hooks
- `internal/search/` index generation
- `internal/server/` dev server + watcher + live reload
- `internal/linkcheck/` optional external link verification
- `internal/utils/` fs/path/slug/url/time/concurrency helpers

All module APIs should be deterministic and unit-testable independently.

Template-engine requirement:

- `internal/templates` must be implemented on top of native Go MiniJinja only.
- Any Tera-compatibility shims should be written in Go as adapter logic around MiniJinja.

---

## 4. Compatibility Targets

### 4.1 Content and Metadata

- TOML and YAML front matter delimiters (`+++` and `---`)
- `_index.md` semantics for sections
- page vs section metadata behavior
- slug/path/permalink generation
- date extraction from RFC3339-prefixed filenames
- colocated assets behavior

### 4.2 Templates

- Template inheritance/extends
- Includes/imports/macros (as supported by MiniJinja)
- Built-in fallback templates (404/feed/sitemap/etc.)
- Theme template override precedence
- Shortcodes (markdown and html modes)

### 4.3 Build Pipeline

Follow Zola ordering constraints:

1. clean output (build mode only)
2. compile Sass
3. build search index
4. render aliases
5. render sections
6. render pages/orphans
7. sitemap/feeds/404/robots/taxonomies
8. process images
9. copy static assets

### 4.4 Serve Mode

- In-memory content cache for fast rebuilds
- filesystem watcher with debounce
- websocket live reload endpoint
- safe static path serving and path traversal protection

---

## 5. Step-by-Step Implementation Plan

## Step 0: Repository bootstrap and standards

1. Initialize Go module and version policy (Go 1.22+ recommended).
2. Set up lint/test/format targets:
   - `go test ./...`
   - `go vet ./...`
   - formatter (`gofmt` or `goimports`).
3. Create package skeleton and dependency lock strategy.
4. Add CI pipeline with matrix for Linux/macOS.
5. Add `CONTRIBUTING.md` conventions:
   - No implicit global state unless read-only cache
   - Stable sorting for all output-sensitive lists
   - Prefer explicit errors with file/path context

Deliverable: compiling empty CLI with placeholder commands and passing CI.

---

## Step 1: CLI contract and command flow

1. Implement root CLI with subcommands:
   - `init`
   - `build`
   - `serve`
   - `check`
2. Implement config discovery logic:
   - prefer `zola.toml`, fallback `config.toml`
   - search current dir + ancestors
3. Implement shared flags:
   - `--root`
   - `--config`
   - command-specific output/base URL/drafts/minify
4. Define command handler interfaces in `internal/site`.
5. Add basic user-facing logging modes (info/warn/error).

Deliverable: CLI behavior and argument surface compatible enough for scripted usage.

---

## Step 2: Config parsing and validation

1. Model config structs from Zola’s `Config` semantics:
   - site metadata/base URL
   - markdown/highlight/search/taxonomies
   - language-specific sections
   - theme/output/static/sass options
2. Parse TOML config and normalize defaults.
3. Implement theme config merge (`themes/<theme>/theme.toml`).
4. Implement config validation pass:
   - required values
   - URL sanity checks
   - conflicting options detection
5. Freeze immutable config snapshot passed to all runtime modules.

Deliverable: validated config object with deterministic defaulting.

---

## Step 3: Front matter extraction engine

1. Implement robust split for front matter + body:
   - TOML `+++ ... +++`
   - YAML `--- ... ---`
2. Preserve behavior for files with front matter only.
3. Emit contextual parse errors: include file path and line offsets if possible.
4. Add fixtures from valid/invalid cases for regression.
5. Return typed metadata structures for page/section separately.

Deliverable: `ParsePageContent` and `ParseSectionContent` APIs with comprehensive tests.

---

## Step 4: Content discovery and model layer

1. Walk `content/` recursively.
2. Classify markdown files into:
   - section `_index(.lang).md`
   - regular pages
3. Build core models:
   - `Page`
   - `Section`
   - `Taxonomy`
   - `Library` (global graph + indices)
4. Compute:
   - language
   - slug/path/components
   - permalink
   - ancestors/children relationships
5. Detect and register colocated assets per content node.

Deliverable: in-memory content graph independent of rendering.

---

## Step 5: Markdown pipeline

1. Select markdown engine (Goldmark recommended).
2. Implement render context structure:
   - current page path/permalink
   - config snapshot
   - permalink map for internal links
   - shortcode registry
3. Implement markdown transformations:
   - summary extraction from `<!-- more -->`
   - heading anchors
   - table of contents extraction
   - internal link rewrite and validation prep
   - external link capture
4. Ensure markdown output can host injected shortcode placeholders safely.

Deliverable: markdown renderer returning `{body, summary, toc, internalLinks, externalLinks}`.

---

## Step 6: Shortcode system

1. Discover shortcode templates in template namespace:
   - `shortcodes/*.html`
   - `shortcodes/*.md`
2. Parse shortcode invocations in content.
3. Implement two-phase render:
   - render markdown shortcodes before markdown conversion
   - render html shortcodes post/inline according to placeholder strategy
4. Maintain invocation counters for deterministic naming.
5. Add recursion and error protection (depth limits, missing shortcode diagnostics).

Deliverable: shortcode extraction + render parity for common Zola sites.

---

## Step 7: MiniJinja integration layer

1. Build template loader for site and theme directories.
2. Implement precedence rules:
   - user templates override theme
   - built-ins as fallback
3. Add fallback resolver for defaults (`page.html`, `section.html`, etc.).
4. Register filters and globals in two phases:
   - early functions for shortcode-time rendering
   - full functions after library is fully loaded
5. Implement strict error wrapping with template name + rendered context hints.

Deliverable: a reusable `TemplateEngine` abstraction backed by MiniJinja.

---

## Step 8: Global template functions and filters

Implement in priority order:

1. URL/content lookup functions:
   - `get_url`
   - `get_page`
   - `get_section`
2. Taxonomy helpers:
   - `get_taxonomy`
   - `get_taxonomy_term`
   - `get_taxonomy_url`
3. Utility helpers:
   - `trans`
   - `now`
   - `load_data`
4. Asset helpers:
   - `resize_image`
   - `get_image_metadata`
   - `get_hash`
5. Core filters:
   - markdown filter
   - base64 encode/decode
   - regex replace
   - numeric formatting

Deliverable: enough function/filter coverage for default and typical custom themes.

---

## Step 9: Site build orchestrator

1. Implement `Site` struct with immutable config + mutable runtime state.
2. Add output writing abstraction supporting:
   - disk mode
   - memory mode
   - dual mode
3. Implement build execution order matching Zola.
4. Renderers to implement:
   - pages
   - sections
   - aliases
   - taxonomies
   - feeds
   - sitemap
   - robots
   - 404
5. Add optional minification and content post-processing hooks.

Deliverable: `Build()` produces full static output for baseline fixture sites.

---

## Step 10: Static assets and Sass

1. Implement static directory copy with overwrite controls.
2. Implement colocated asset copy adjacent to rendered content outputs.
3. Integrate Sass compilation strategy:
   - invoke external tool or embedded compiler
   - support theme and site Sass directories
4. Implement CSS theme generation hooks for syntax highlighting if configured.
5. Ensure deterministic output paths and conflict behavior.

Deliverable: static and stylesheet behavior aligned with common Zola usage.

---

## Step 11: Search index generation

1. Implement index model from content library.
2. Support at least one format first (recommend Fuse JSON) for MVP.
3. Add Elasticlunr/further formats in subsequent iteration.
4. Honor language-specific search index settings.
5. Validate generated index against fixture snapshots.

Deliverable: searchable site artifacts generated during build.

---

## Step 12: Link checking module (check command)

1. Implement extraction of collected external links from rendered content metadata.
2. Build HTTP client with connection reuse and configurable timeout.
3. Cache URL check results for single run and optional persisted cache.
4. Implement anchor checks for fetched HTML where configured.
5. Emit report with warn/error levels per config.

Deliverable: `check` command with reliable diagnostics and CI-friendly exit codes.

---

## Step 13: Dev server and live reload

1. Implement static file server with safe canonical path checks.
2. Add in-memory content serving path for fast serve mode.
3. Add websocket endpoint for reload messages.
4. Integrate filesystem watcher with debounce and targeted rebuild triggers.
5. Implement reload broadcast for changed paths and fallback full reload.

Deliverable: productive local `serve` workflow comparable to Zola.

---

## Step 14: Performance and concurrency

1. Profile build hotspots:
   - markdown render
   - template render
   - image processing
   - filesystem writes
2. Add controlled worker pools where safe.
3. Ensure deterministic ordering despite concurrency.
4. Add caching boundaries:
   - parsed content cache
   - template cache
   - link check cache
5. Add benchmark fixtures and compare across releases.

Deliverable: stable performance baseline with reproducible outputs.

---

## Step 15: Test strategy and acceptance gates

1. Unit tests for all parser/slug/path/link helpers.
2. Golden tests for markdown output and shortcode behavior.
3. Integration fixture sites:
   - simple blog
   - taxonomy-heavy site
   - multilingual site
   - theme-based site
4. Snapshot output directory tests against expected artifacts.
5. Add CLI E2E tests for `build`, `serve` smoke checks, and `check`.

Deliverable: release gate requiring all fixture sites to build successfully.

---

## 6. Milestones

## Milestone A: MVP Build Compatibility

Includes Steps 0-9 with reduced helper/filter set. Outcome:

- can parse config and content
- can render pages/sections with MiniJinja
- can produce static output for non-trivial sites

## Milestone B: Production-Ready Build

Adds Steps 10-12. Outcome:

- Sass/static/search/link-check supported for normal CI workflows

## Milestone C: Full Dev Experience

Adds Step 13 and baseline Step 14 optimization. Outcome:

- robust `serve` and iterative editing loop

## Milestone D: Parity and Hardening

Completes advanced filters/functions, deep i18n, performance, and edge-case parity.

---

## 7. Risks and Mitigation

1. **Template behavior mismatch (Tera vs MiniJinja)**
   - Mitigation: compatibility shim layer + focused template fixture tests.
2. **Shortcode edge-case divergence**
   - Mitigation: replicate two-phase processing and placeholder semantics exactly.
3. **Path/permalink inconsistencies**
   - Mitigation: centralize path logic and golden-test expected URLs.
4. **Watcher instability across platforms**
   - Mitigation: abstract watcher backend and test on macOS/Linux CI.
5. **Incremental feature creep**
   - Mitigation: strict milestone gates and deferred non-goals tracking.

---

## 8. Deliverables Checklist

- [ ] CLI with `init`, `build`, `serve`, `check`
- [ ] Config parser + validation + theme merge
- [ ] Content library model with pages/sections/taxonomies
- [ ] Markdown + shortcode pipeline
- [ ] MiniJinja template engine integration and fallback handling
- [ ] Build orchestrator matching Zola step order
- [ ] Static/Sass/search/link checking modules
- [ ] Live reload dev server
- [ ] Fixture-based integration tests
- [ ] Documentation for migration and known incompatibilities

---

## 9. Implementation Sequencing Recommendation

Use this exact coding sequence for fastest risk reduction:

1. Steps 0-2 (foundation)
2. Steps 3-4 (content graph)
3. Steps 5-7 (rendering core)
4. Step 9 (full build skeleton)
5. Step 6 and Step 8 completion (shortcodes/helpers)
6. Steps 10-11 (assets/search)
7. Steps 12-13 (check/serve)
8. Steps 14-15 (optimize/harden)

This order gets working builds early while isolating higher-complexity compatibility work.
