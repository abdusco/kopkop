# Zola Test Porting Plan (Rust -> Go)

## Goal

Build a parity-focused Go test suite that uses Zola’s existing tests and fixtures as the source of truth, so correctness is validated continuously during the port.

---

## Principles

- Prefer behavior parity over implementation parity.
- Start with integration snapshot tests for maximum confidence early.
- Port unit tests for logic-heavy modules next.
- Track known template-engine differences explicitly (Tera vs MiniJinja).
- Keep tests deterministic, OS-portable, and CI-friendly.
- The template engine under test must be the native Go MiniJinja port (`minijinja-go`), not Rust MiniJinja via FFI/cgo.

---

## Test Strategy Overview

We will run **three complementary test layers**:

1. **Go-native unit tests**
   - Port logic tests from Zola Rust modules into Go package tests.
   - Validate parsers, slug/permalink generation, front matter, link logic, and sorting.

2. **Go-native integration tests (snapshot-based)**
   - Build fixture sites with the Go binary.
   - Compare output trees with expected snapshots.

3. **Differential parity tests (Go vs Zola)**
   - Build the same fixture with both binaries.
   - Normalize outputs and compare generated artifacts.
   - Use this to identify regressions and intentional divergence.

---

## Proposed Directory Layout

```text
tests/
  fixtures/
    zola/                 # copied or mirrored fixture sites from upstream
  snapshots/
    go-expected/          # expected outputs for Go integration tests
  parity/
    known_diffs.yaml      # intentional differences, with justification
  harness/
    runner_test.go        # shared build/compare utilities
    normalize.go          # HTML/JSON/XML normalization helpers
internal/
  ... package tests ...
```

---

## Step-by-Step Execution Plan

## Step 1: Build test harness foundation

1. Create a reusable test helper package (`tests/harness`) with functions to:
   - run Go binary build for a fixture
   - run Zola binary build for the same fixture
   - capture stdout/stderr/exit code
   - collect output directories recursively
2. Add file compare helpers:
   - byte compare for exact files (css/js/assets)
   - normalized compare for html/json/xml/txt
3. Add ignore rules for known non-deterministic files (timestamps, build metadata).
4. Add CI flags/env vars:
   - `PARITY_ZOLA_BIN` path to zola executable
   - `PARITY_RUN_DIFF=1` to enable differential tests

Deliverable: reusable cross-runner harness in place.

---

## Step 2: Import and curate fixture sites

1. Copy high-value fixture sites from upstream Zola repository:
   - `test_site`
   - `test_site_i18n`
   - selected invalid fixture cases from `test_sites_invalid`
2. Document fixture provenance (upstream commit hash) in a metadata file.
3. Remove or tag fixtures relying on features not yet implemented in Go.
4. Keep fixtures read-only in normal development flow.

Deliverable: stable fixture corpus with provenance and support status.

---

## Step 3: Add first integration snapshots (Go-only)

1. For each MVP fixture:
   - run Go build
   - store generated output snapshot in `tests/snapshots/go-expected`
2. Implement snapshot assertion tests:
   - compare directory trees
   - verify key files (index pages, taxonomies, feeds)
3. Add snapshot update command (manual, explicit):
   - `go test ./... -update-snapshots`
4. Require clean snapshots in CI.

Deliverable: deterministic Go integration coverage.

---

## Step 4: Add differential tests (Go vs Zola)

1. Build each fixture with Zola and Go.
2. Compare outputs after normalization:
   - HTML: normalize whitespace/attribute ordering where needed
   - JSON: canonical formatting (sorted keys)
   - XML: canonicalized formatting
3. Record expected diffs in `tests/parity/known_diffs.yaml` with:
   - file path pattern
   - reason
   - target milestone to fix
4. Fail on any unclassified diff.

Deliverable: parity gate that catches semantic drift early.

---

## Step 5: Port high-value Rust unit tests to Go

Start with behavior-critical modules:

1. Front matter splitting/parsing
2. Slug/path/permalink generation
3. Link resolution and anchor handling
4. Taxonomy and sorting logic
5. Markdown summary and heading anchor behavior
6. Shortcode parsing (both md/html shortcode forms)

For each ported test:

- preserve original test case name when possible
- add comment with upstream source file/function reference
- ensure table-driven style for maintainability

Deliverable: robust unit-level parity for foundational logic.

---

## Step 6: Expand to serve/check behavior tests

1. Add `serve` smoke tests:
   - starts server
   - returns expected pages
   - path traversal protection
2. Add live reload protocol tests:
   - websocket connection
   - emits reload message on file change
3. Add `check` command tests:
   - internal link errors
   - external link status handling (mock HTTP server)
   - anchor existence checks

Deliverable: confidence in non-build command correctness.

---

## Step 7: CI and quality gates

1. Define test tiers:
   - Tier 1 (PR required): unit + core integration snapshots
   - Tier 2 (daily/nightly): full differential suite against Zola
2. Add coverage reporting for key modules.
3. Add flaky-test policy:
   - quarantine with issue id
   - no silent retries without triage
4. Enforce parity dashboard updates in PR template.

Deliverable: sustainable and enforceable correctness pipeline.

---

## Initial 20 Tests to Port First

## A. Front matter and parsing (6)

1. TOML front matter with body
2. YAML front matter with body
3. Front matter only (no body)
4. Invalid front matter delimiter error
5. Front matter containing delimiter-like content in body
6. Path-aware error message includes file name

## B. Slug/path/permalink (5)

7. Default slug from filename
8. Slug override in front matter
9. Dated filename parsing and date extraction
10. Path override behavior
11. Section/page permalink composition

## C. Markdown and links (4)

12. `<!-- more -->` summary extraction
13. Heading anchor generation with duplicate headings
14. Internal `@/` link resolution
15. Colocated asset link rewriting

## D. Shortcodes and templates (3)

16. Markdown shortcode rendered pre-markdown
17. HTML shortcode placeholder replacement
18. Missing shortcode raises useful error

## E. Integration/parity smoke (2)

19. `test_site` full build snapshot
20. `test_site_i18n` multilingual build snapshot

---

## Known Differences Tracking Format

Create `tests/parity/known_diffs.yaml` with entries like:

```yaml
- id: tpl-whitespace-001
  pattern: "**/*.html"
  reason: "MiniJinja whitespace behavior differs from Tera in block trimming"
  classification: "intentional-temporary"
  target_milestone: "Milestone D"
  owner: "templating"
```

Rules:

- Every diff must have an id and milestone.
- No wildcard "ignore everything" entries.
- Expired milestone entries should fail CI.

---

## Tooling Recommendations

- Go test helpers: `testing`, `t.TempDir`, table-driven tests.
- Snapshot compare: custom helper or a lightweight snapshot lib.
- HTML normalization: parse + re-serialize via Go HTML parser.
- JSON normalization: unmarshal + marshal canonical with sorted keys.
- External HTTP behavior: use `httptest.Server`.

Template stack note:

- Differential and integration tests should validate behavior produced by `github.com/mitsuhiko/minijinja/minijinja-go/v2` specifically.
- Do not introduce alternate template backends in parity tests, or results become non-actionable for the target architecture.

---

## Success Criteria

We consider test-porting successful when:

1. Core fixture builds pass consistently in CI.
2. Differential tests report only tracked known diffs.
3. High-risk logic modules have direct unit parity tests.
4. New regressions are caught by tests before release.

---

## Immediate Next Actions

1. Implement `tests/harness` runner and normalizer utilities.
2. Import `test_site` and `test_site_i18n` fixtures with provenance metadata.
3. Add the first 2 integration snapshot tests.
4. Port the first 6 front matter parsing tests.
5. Add `known_diffs.yaml` and enforce “no untracked diff” in differential runs.
