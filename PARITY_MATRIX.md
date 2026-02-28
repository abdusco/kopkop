# Parity Matrix

Status legend:

- `done`: implemented and covered by tests
- `partial`: implemented with known gaps
- `todo`: not implemented yet

## CLI

- `init`: done
- `build`: done
- `serve`: done
- `check`: done

## Config

- config discovery (zola.toml/config.toml): done
- config parsing and defaults: done
- theme merge: partial
- full validation parity: partial

## Content

- front matter split (toml/yaml): done
- page/section load: done
- drafts filtering: done
- taxonomy graph: done
- multilingual edge cases: partial

## Markdown

- summary divider: done
- heading anchors + duplicate handling: done
- internal/external link collection: done
- full Zola markdown semantics parity: partial

## Shortcodes

- parser (inline/body/ignored/nested): done
- md/html insertion phases: done
- full runtime context parity: partial

## Templates (MiniJinja-Go)

- environment setup with strict undefined: done
- site/theme/builtin fallback: done
- helper/filter registration: partial

## Build Pipeline

- output clean/write modes: done
- page/section rendering: done
- aliases/404/robots/sitemap/feed: done
- search index generation: done
- static + colocated assets: done
- image processing parity: todo

## Serve Pipeline

- watch + debounce: done
- websocket livereload: done
- path traversal protection: done

## Link Checking

- external status checks: done
- anchor checks: done
- parity with all zola checker options: partial

## Test/Parity Infrastructure

- unit tests for core modules: done
- vendored fixture integration tests: done
- optional differential parity tests vs zola: done
