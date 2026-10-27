# 0001 — Rust, blank slate, no behavioural port

Status: accepted · 2026-10-27

## Context

v1 is Go. Its core had no layer boundaries: parsing, semantics, tool
integration and output were entangled, and every attempt to add a plugin
layer on top inherited the entanglement. A first Rust attempt (Oct 2026)
re-typed the same structure and was discarded.

## Decision

v2 is written in Rust from an empty tree. No file, type, or test is ported
from v1 or from the first Rust attempt. Behavioural parity with v1 is not a
requirement. It may become a later goal for output formats only.

## Consequences

- No migration shims, no dual-stack period, no "bridge" code.
- Rule coverage starts at zero and grows per ADR-0007/0009 fixtures.
- v1 stays published until v2 reaches a release gate defined separately.

## Open

- None.
