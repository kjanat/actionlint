# actionlint v2 — design

Blank-slate redesign. Nothing here derives from the v1 Go tree or from the
first Rust attempt. Both were a workflow linter with a plugin layer bolted on.
This is a semantic model of GitHub Actions with consumers.

- [`design/architecture.md`](design/architecture.md) — layers, data flow, identity, diagnostics
- [`design/repo-layout.md`](design/repo-layout.md) — crates, dependency direction, what goes where
- [`design/rules.md`](design/rules.md) — rule ids, areas, categories, config, suppression
- [`adr/`](adr/README.md) — decisions, one per file, numbered

Reading order: architecture → repo-layout → ADRs.

Rule for every future change: if it cannot be placed in a layer from
`architecture.md` without widening that layer's contract, it is in the wrong
place. Stop and write an ADR first.
