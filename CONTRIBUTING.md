# Contributing

## Development Workflow

1. Keep changes small and test-first where possible.
2. Run `make ci` before submitting changes.
3. Add tests for any behavior change.

## Coding Rules

- Prefer explicit errors with context (file path, operation, reason).
- Avoid hidden mutable global state in core modules.
- Keep output ordering deterministic (sort paths/keys before rendering where needed).
- Preserve template and content compatibility with Zola unless intentionally documented.

## Parity Rules

- Differential parity tests are optional locally and enabled with:
  - `PARITY_RUN_DIFF=1`
  - `PARITY_ZOLA_BIN=/path/to/zola`
- Record intentional differences in `tests/parity/known_diffs.yaml`.
