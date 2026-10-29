# Language server readiness

Status: source audit of this checkout, 2026-10-27. **Implemented** means the
`actionlint lsp` server advertises and handles the feature. **Partial** means
useful code or editor support exists, but the stated LSP feature is incomplete.
Editor grammar features are separate from server capabilities.

Run `actionlint lsp` over stdin/stdout. The server uses UTF-16 positions,
versioned open documents, incremental changes, captured source revisions, and
cancelled analysis generations. See [transport](../internal/langserver/server.go),
[editor integration](../internal/localapp/lsp.go), and
[usage](usage.md#editor-fixes-and-remote-resolution).

| Feature                          | Status      | Evidence and missing work                                                                                                                                                                                                                                                                                                                                                                                       |
| -------------------------------- | ----------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Syntax highlighting              | External    | The server advertises no color provider or semantic tokens. An editor grammar can highlight YAML and expressions; this repository supplies no such grammar.                                                                                                                                                                                                                                                     |
| Folding                          | Absent      | No `textDocument/foldingRange` handler. Project owned YAML syntax nodes exist in [`internal/yaml/syntax`](../internal/yaml/syntax); project valid source ranges before advertising it.                                                                                                                                                                                                                          |
| Syntax tree                      | Partial     | The YAML parser and immutable workflow declarations exist, but there is no LSP tree view or custom tree request. Expose a documented editor tree only if a client contract is chosen.                                                                                                                                                                                                                           |
| Selection ranges                 | Absent      | No `textDocument/selectionRange` handler. Project nested YAML/expression ranges from captured text.                                                                                                                                                                                                                                                                                                             |
| Diagnostics                      | Implemented | [`Serve`](../internal/langserver/server.go) publishes current and dependent document diagnostics; [publication](../internal/langserver/publication.go) maps severity, code, links and related locations. Stale generations cannot publish.                                                                                                                                                                      |
| Semantic tokens                  | Absent      | No legend, full/range token handler or invalidation. Tokenize owned syntax and expressions before advertising tokens.                                                                                                                                                                                                                                                                                           |
| Completion                       | Partial     | [Editor completion](../internal/workflow/editor/completion.go) supplies expression roots/properties, event payload fields and `workflow_dispatch` inputs from the open buffer; [transport](../internal/langserver/completion.go) maps edits to UTF-16. It does not receive the analysis checker's job, matrix, step, reusable-workflow or action metadata types, nor complete YAML keys/values or `uses:` refs. |
| Definition / declaration         | Absent      | No navigation handlers. Resolve source locations for local action/workflow refs and workflow identifiers; only then add remote targets whose source is captured and addressable.                                                                                                                                                                                                                                |
| Hover                            | Absent      | No `textDocument/hover` handler. Reuse expression types and rule metadata with captured source positions; do not perform an independent network fetch in a request handler.                                                                                                                                                                                                                                     |
| Document symbols                 | Partial     | Analysis already carries [document outlines](design/analysis-contracts.md); no `textDocument/documentSymbol` handler projects them to LSP.                                                                                                                                                                                                                                                                      |
| Dependency tree                  | Partial     | Analysis tracks dependencies for checks and diagnostic invalidation, but no LSP tree or custom request exposes a dependency graph. Define node identities, provenance and editor UI first.                                                                                                                                                                                                                      |
| References / document highlights | Absent      | No `textDocument/references` or `textDocument/documentHighlight` handlers. A source-backed identifier/ref index and scope rules are required.                                                                                                                                                                                                                                                                   |
| Workspace symbols                | Absent      | No `workspace/symbol` handler or workspace document index. Open-buffer analysis alone cannot answer workspace-wide requests.                                                                                                                                                                                                                                                                                    |
| Assists / code actions           | Partial     | [Quick fixes](../internal/langserver/actions.go) expose valid diagnostic fixes as versioned `documentChanges`. No refactors, fix-all, action update/pin assists, or code-action resolve handler.                                                                                                                                                                                                                |

The [server switch](../internal/langserver/server.go) advertises only document
sync, code actions and optional completion. Unsupported requests return LSP
`-32601`. The capabilities above are therefore a server checklist, not a claim
about what a particular editor may supply on its own.

## Inference and network boundary

The diagnostic path already infers `matrix`, `steps`, `needs`, `inputs`, `jobs`
and action/workflow outputs through
[`internal/workflow/expressions`](../internal/workflow/expressions). Local action
metadata, popular action metadata and reusable-workflow metadata enter its
[`Dependencies`](../internal/workflow/expressions/checker.go). Unknown action
outputs remain an open string map in
[`dependency_types.go`](../internal/workflow/expressions/dependency_types.go).
Current editor completion instead constructs a fresh expression checker from
the buffer. Thus diagnostic inference does **not** imply equivalent completion,
hover or navigation.

The current [`application.Check`](../internal/application/check.go) accepts an injected
[`resolver.Resolver`](../resolver/resolver.go), creating a fresh
`resolver.SessionFactory` session per analysis. This resolves references for
diagnostics and pin proposals; it does not currently supply remote action
input/output schemas or editor navigation locations. This is incomplete relative
to the requested default: remote metadata typing must be enabled by default with
an explicit opt-out, reliable caching and permissions, per
[#157](https://github.com/kjanat/actionlint/issues/157#issuecomment-6353123385).
The provider and LSP integration is tracked in
[#336](https://github.com/kjanat/actionlint/issues/336). Explicit offline mode must
prevent network requests; missing cached metadata and provider failures must be
reported rather than treated as successful validation. The current injected
resolver is not implementation of that default-on metadata service.

Plugin and engine selection is held by the
[`editorSession`](../internal/localapp/editor_session.go): explicit plugins and
remote engine URLs are startup snapshots; configured local artifacts are
content identified. Plugin rules contribute diagnostics/fixes, not general LSP
feature providers. Do not describe networked type inference as implemented.

For the concrete extension path and acceptance checks, see the
[langserver implementation notes](../internal/langserver/README.md).
