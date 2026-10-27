# 0008 — No external-tool integration in this repository

Status: accepted · 2026-10-27

## Context

v1 shipped ShellCheck, Ruff, and pyflakes adapters in-tree: per-tool
config keys, CLI flags, version probes, output parsers, fallback planning,
Nix packaging, Docker tests. Each adapter was a bespoke coupling because no
semantic model existed to couple to. Maintaining them consumed more effort
than the core.

## Decision

This repository contains no code, config key, flag, fixture, or packaging
that names a specific external tool. Script analysis is exposed as
providers (`scripts(DocumentId, Occurrence) -> [Script { lang, shell,
text, at }]`) that any extension may consume. Running ShellCheck over those
scripts is an extension's job, in its own repository, or nobody's.

## Consequences

- `internal/tools/*`, `.shellcheckrc` handling, `nix/ruff*`, Docker tool
  tests, and `tools.*` config keys do not return.
- Script extraction, shell inference (`defaults.run.shell`, runner OS,
  step `shell:`), and line/column mapping into the YAML scalar are core
  providers because they are facts about the workflow, not about a tool.
- Ecosystem acceptance (does a third-party ShellCheck extension work?) is an
  integration test that lives with that extension.

## Open

- None.
