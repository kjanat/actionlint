# 0011 — CLI exit codes and bare-invocation behaviour

Status: accepted · 2026-10-27

## Context

v1's bare `actionlint` ran analysis on CWD and exited non-zero when nothing
was found to analyse, which broke "is it installed" checks and surprised
new users. Subcommands were implicit.

## Decision

`clap`, derive API, subcommands explicit.

| invocation                  | behaviour                                                                                     | exit                                                                                                         |
| --------------------------- | --------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `actionlint`                | print help to stdout                                                                          | 0                                                                                                            |
| `actionlint check [PATHS…]` | analyse. Default paths `.github/workflows`, `action.yml`, `action.yaml`, `.github/actions/**` | 0 clean · 1 findings at or above `--fail-on` (default `error`) · 2 usage/config error · 3 nothing to analyse |
| `actionlint check --fix`    | apply safe fixes, then report remaining                                                       | as above                                                                                                     |
| `actionlint fetch`          | populate the `uses:` cache (ADR-0006)                                                         | 0 / 2                                                                                                        |
| `actionlint lsp`            | start the language server on stdio                                                            | —                                                                                                            |
| `actionlint schema`         | print config JSON schema                                                                      | 0                                                                                                            |
| `actionlint rules [ID]`     | list rules / show one rule's doc                                                              | 0                                                                                                            |

- `--format text|json|sarif|github` (`github` = workflow-command
  annotations for the Action).
- Colour: auto by TTY, `--color`, `NO_COLOR` honoured.
- Exit 3 is distinct so CI can treat "no workflows here" as skip rather
  than failure.

## Consequences

- `action.yml` wraps `actionlint check --format github`. Maps 3 → 0 by
  default with an input to make it fail.
- No hidden default command. Scripts must say `check`.

## Open

- `--fail-on` default: `error` or `warning`? Leaning `error`.
