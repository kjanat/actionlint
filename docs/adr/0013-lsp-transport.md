# 0013 — LSP transport: `lsp-server` + `gen-lsp-types`, synchronous loop

Status: accepted · 2026-10-27 · verified against crates.io and project Cargo.toml files (2026-10-27)

## Context

State of the Rust LSP crates:

| crate                               | version              | last release      | status                                                                           |
| ----------------------------------- | -------------------- | ----------------- | -------------------------------------------------------------------------------- |
| `tower-lsp`                         | 0.20.0               | 2023-08-11        | unmaintained, maintainer-interest issue #427 open since 2024-08 with no response |
| `tower-lsp-server` (community fork) | 0.23.0 / 0.24.0-rc.1 | 2025-12 / 2026-09 | active, used by biome (0.23.0) and oxc (0.24.0-rc.1)                             |
| `lsp-server` (rust-analyzer)        | 0.10.0               | 2026-07-16        | active, sync `crossbeam-channel` API, used by ruff                               |
| `lsp-types` (gluon-lang)            | 0.97.0               | 2024-06-04        | stale                                                                            |
| `gen-lsp-types`                     | 0.11.0               | 2026-07-28        | generated from the LSP metamodel, used by ruff and by `tower-lsp-server`         |
| `async-lsp`                         | 0.2.4                | 2026-04-24        | active, but still pins `lsp-types 0.95`                                          |

[crates.io API, github.com/ebkalderon/tower-lsp/issues/427, ruff, biome,
oxc, tower-lsp-server `Cargo.toml` on main]

## Decision

`lsp-server = "=0.10.0"` + `lsp-types = { package = "gen-lsp-types",
version = "=0.11.0" }`. Our own dispatch loop.

Reason: the salsa cancellation model (ADR-0004) is "one storage handle per
in-flight request, an edit takes `&mut db` and cancels them all". That is
a synchronous, thread-per-request shape — exactly what rust-analyzer and
ruff run on `lsp-server`. A tower/async stack adds an executor between
the request and the database for no benefit. `tower-lsp` itself is dead.

## Consequences

- Request handling: main loop reads `lsp_server::Message`, snapshots the
  db, spawns the handler on a small thread pool, writes the response.
  Cancellation on edit via `Cancelled::catch` → `RequestCancelled` reply.
- `gen-lsp-types` tracks the metamodel, so new LSP features arrive without
  waiting on a hand-written types crate.
- No `tokio` in `lsp`. If some future feature needs async I/O it gets its
  own thread, not an executor in the hot path.

## Virtual remote documents

The server owns a read-only URI scheme, `actionlint:`, and serves
documents under it the way gopls (`gopls://`) and deno (`deno:`) do.
Navigation never leaves the editor for a GitHub homepage.

| request                            | on                                             | resolves to                                                                                                                                                                                                                                                                                                                                                                                                                                |
| ---------------------------------- | ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `textDocument/definition`          | `uses: owner/repo[/path]@ref`                  | `actionlint:action/<repo-id>/<sha>/<path>/action.yml` — `<repo-id>` is the repository's numeric id (commitish `Pin` keeps host, numeric id, observed name), so `Actions/Checkout` and `actions/checkout` yield one URI. `path` may contain `@`; the sha is a fixed-width hex segment so parsing is positional, not split-on-`@`                                                                                                            |
| `textDocument/definition`          | `uses: owner/repo[/path]@ref`, Dockerfile-only | `actionlint:action/<repo-id>/<sha>/<path>/Dockerfile`. The runner's lookup order is `action.yml`, `action.yaml`, `Dockerfile`, `dockerfile` (`Runner.Worker/ActionManager.cs:727-728`, error text at `:912`); an action with no manifest and a Dockerfile is a container action and v1 modelled it as outcome `opaque(dockerfile-only)`. Definition opens the Dockerfile; hover shows "Dockerfile-only action, no declared inputs/outputs" |
| `textDocument/definition`          | `uses: docker://image:tag`                     | none. A pulled image has no `Dockerfile` to show. Hover gets the image reference and resolved digest only                                                                                                                                                                                                                                                                                                                                  |
| `textDocument/definition`          | `uses: ./path`, `$/path`                       | the local file, ordinary `file:` URI                                                                                                                                                                                                                                                                                                                                                                                                       |
| `textDocument/definition`          | `uses: owner/repo/.github/workflows/x.yml@ref` | `actionlint:workflow/owner/repo/.github/workflows/x.yml@<sha>`                                                                                                                                                                                                                                                                                                                                                                             |
| `textDocument/hover`               | the same                                       | inputs/outputs table (typed, from the same data `type_env` uses, ADR-0016) + the README at that ref, truncated                                                                                                                                                                                                                                                                                                                             |
| `textDocument/definition`, `hover` | a workflow key, context name, function, event  | `actionlint:docs/<vendor-sha>/<slug>` — the docs.github.com page for that symbol, fetched from the `api/article/body` markdown endpoint at vendoring time                                                                                                                                                                                                                                                                                  |
| `textDocument/documentLink`        | every `uses:`                                  | the `actionlint:` target, so clients without definition support still get a link                                                                                                                                                                                                                                                                                                                                                           |

- Content comes from `resolve_uses` and the action catalog (ADR-0006,
  ADR-0016). Usually the bytes are already cached by the time hover or
  definition asks. When they are not, the request handler does not
  block on the socket: it answers with what it has (`Unresolved
  (not-fetched)` renders as "fetching …"), the fetch runs on the
  `workspace` network thread, and the server re-publishes diagnostics /
  the client re-requests once the cache fills. Offline → the cached ref
  or the `unavailable` rendering.
- Two properties, kept apart: **read-only** (the client cannot edit) and
  **immutable identity** (one URI always denotes the same bytes). Every
  `actionlint:` URI has both, because every URI embeds the revision of
  what it denotes: the numeric repository id plus the resolved commit sha
  for actions and workflows (one content, one URI — the observed name
  is not identity, GitHub repo names are case-insensitive and renamable),
  the vendored-docs sha (`semantics/vendor/GITHUB.lock`, ADR-0014) for
  docs. Re-vendoring the docs produces new URIs. An old one still
  resolves to the old bytes while they are cached, and to "gone" after.
  No `actionlint:` URI is ever live.
- Content delivery is a custom request, because LSP has no way for a
  server to push a document body: `textDocument/didOpen` is a
  client → server *notification* about a buffer the client already
  holds, not a fetch. The client needs a content provider for the scheme
  (VS Code: `TextDocumentContentProvider`, other editors have their
  equivalents) that calls
  `actionlint/virtualTextDocument { textDocument: { uri } } → string`
  — the same shape as deno's `deno/virtualTextDocument`, which routes
  every `deno:` URI through one request. Answered from the cache (filled
  by `resolve_uses` over the network earlier, or now, off-thread), never
  entering the salsa input set. A client
  without that provider gets the `documentLink`/`definition` URI and
  cannot open it. That is the known limit, not a fallback.
- `actionlint:/status.md` (deno's `deno:/status.md`) renders server
  status: pinned shas, cache sizes, loaded extensions.
- docs.github.com serves every page as markdown two ways, both verified
  2026-10-28 on the workflow-syntax page:
  1. the article API: `GET https://docs.github.com/api/article/body?pathname=<page path>`
     → `200 text/markdown; charset=utf-8`. It also accepts a *moved*
     pathname (`/en/actions/writing-workflows/workflow-syntax-for-github-actions`,
     now living at `/en/actions/reference/workflows-and-actions/workflow-syntax`)
     and returns the current body without a redirect. **This is the route
     the server uses.**
  2. content negotiation on the page URL: `Accept: text/markdown` →
     `200 text/markdown` after following the redirect. Fallback only.
     The `.md` suffix form is 404. Slugs are a vendored table keyed by
     schema definition name / context name / function name (ADR-0014 data),
     not scraped at runtime, and `xtask vendor-github` re-checks each
     pathname on every update.
- Prior art in-house: actup's LSP renders inputs/outputs then README in
  hover (`crates/lsp/src/hover.rs`, `Provider::get_readme`). Same spirit,
  hover-only. This ADR adds definition targets, a URI scheme, and the
  content request. Prior art outside: deno `deno/virtualTextDocument`
  [docs.deno.com/runtime/reference/lsp_integration].

## Open

- None.
