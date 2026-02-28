# Porting Status

This document tracks what "ported" means for `kopkop` relative to Zola fixtures.

## Scope

- Target: behavioral parity for current vendored fixtures in `tests/fixtures/zola/`.
- Focus: content loading, templating, i18n, feeds, sitemap, and search artifacts used by those fixtures.
- Out of scope (for now): claiming full parity for every Zola version and all optional features.

## Current Status

- `go test ./...` is green.
- Optional differential parity test is green:
  - `PARITY_RUN_DIFF=1 PARITY_ZOLA_BIN=/Users/abdus/bin/zola go test ./tests/parity -run TestGoVsZolaDifferential_Optional -count=1`
- `tests/parity/known_diffs.yaml` is empty.

## Intentional Normalization in Parity Harness

Parity compares normalized outputs to avoid false negatives from non-semantic differences.

- HTML
  - Collapses whitespace.
  - Normalizes ordering of `<article>` blocks under `<div class="list-posts">` (upstream output can vary by run/filesystem order).
  - Normalizes ordering of repeated `Translated in <lang>:` blocks.
- XML
  - Ignores XML prolog/directive tokens and insignificant whitespace-only char data.
- JS (search assets)
  - `elasticlunr.min.js` compared as library-presence marker.
  - Search index JS normalized to semantic document identity (`permalink|title` for simple index; `id|title` for lunr payload).

## Guardrails

Normalization has dedicated tests in `tests/harness/normalize_test.go` to ensure we only ignore ordering/noise and still catch semantic content changes.

## Definition of Done (Fixture Scope)

- Differential parity green with empty known-diffs file.
- No debug/fallback artifacts leaking into generated outputs.
- Normalization rules documented and covered by tests.
- Any new fixture added must pass or include explicit rationale for normalization/known diff.
