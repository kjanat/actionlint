# Language server readiness

Status: source audit of this checkout, 2026-10-27. **Implemented** means the
`actionlint lsp` server advertises and handles the feature. **Partial** means
useful code or editor support exists, but the stated LSP feature is incomplete.
Editor grammar features are separate from server capabilities.

Run `actionlint lsp` over stdin/stdout. The server uses UTF-16 positions,
versioned open documents, incremental changes, captured source revisions, and
cancelled analysis generations. See [transport](../internal/lsp/server.go),
[editor integration](../internal/lsp/checks.go), and
[usage](usage.md#editor-fixes-and-remote-resolution).

| Feature                          | Status      | Evidence and missing work                                                                                                                                                                                                                                                                                                                                                                               |
| -------------------------------- | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Syntax highlighting              | External    | The server advertises no color provider or semantic tokens. An editor grammar can highlight YAML and expressions; this repository supplies no such grammar.                                                                                                                                                                                                                                             |
| Folding                          | Absent      | There is no `textDocument/foldingRange` handler. The YAML parser supplies syntax nodes in [`internal/languages/yaml/syntax`](../internal/languages/yaml/syntax). Folding support needs valid source ranges before the server advertises it.                                                                                                                                                             |
| Syntax tree                      | Partial     | The YAML parser and immutable workflow declarations exist, but there is no LSP tree view or custom tree request. Exposing a tree requires a documented protocol agreed with the editor client.                                                                                                                                                                                                          |
| Selection ranges                 | Absent      | There is no `textDocument/selectionRange` handler. Selection support needs nested YAML and expression ranges from the captured text.                                                                                                                                                                                                                                                                    |
| Diagnostics                      | Implemented | [`Serve`](../internal/lsp/server.go) publishes diagnostics for current documents and their dependencies. [Diagnostic conversion](../internal/lsp/diagnostics.go) maps severity, code, links and related locations. Superseded analysis generations cannot publish results.                                                                                                                              |
| Add diagnostic suppressions      | Implemented | Quick fixes add or extend YAML suppression comments in open buffers, preserving reasons and targeting block headers or originating declarations. The suppression policy must permit the edit, and the captured revision must match.                                                                                                                                                                     |
| Semantic tokens                  | Absent      | The server has no token legend, full or range token handlers, or token invalidation. These require tokenizing the parsed YAML and expressions before the server advertises support.                                                                                                                                                                                                                     |
| Completion                       | Partial     | [Editor completion](../internal/lsp/completion/completion.go) supplies expression roots/properties, event payload fields and `workflow_dispatch` inputs from the open buffer; [transport](../internal/lsp/completion.go) maps edits to UTF-16. It does not receive the analysis checker's job, matrix, step, reusable-workflow or action metadata types, nor complete YAML keys/values or `uses:` refs. |
| Definition / declaration         | Absent      | There are no navigation handlers. Support first needs source locations for local action and workflow references and workflow identifiers. Remote targets also need captured source that the editor can address.                                                                                                                                                                                         |
| Hover                            | Absent      | There is no `textDocument/hover` handler. It should use existing expression types and rule metadata with captured source positions, without independently fetching data from the network in the request handler.                                                                                                                                                                                        |
| Document symbols                 | Partial     | Analysis already includes [document outlines](design/analysis-contracts.md), but no `textDocument/documentSymbol` handler converts them to LSP symbols.                                                                                                                                                                                                                                                 |
| Dependency tree                  | Partial     | Analysis tracks dependencies for checks and diagnostic invalidation, but no LSP tree or custom request exposes them. A tree view first needs defined node identities, source origins and an editor interface.                                                                                                                                                                                           |
| References / document highlights | Absent      | There are no `textDocument/references` or `textDocument/documentHighlight` handlers. They need an index of identifiers and references tied to captured source locations, with defined scope rules.                                                                                                                                                                                                      |
| Workspace symbols                | Absent      | There is no `workspace/symbol` handler or workspace document index. Analyzing open buffers alone cannot answer requests across the workspace.                                                                                                                                                                                                                                                           |
| Assists / code actions           | Partial     | [Quick fixes](../internal/lsp/actions.go) expose valid diagnostic fixes as versioned `documentChanges`. The server has no refactors, fix-all, action update or pin assists, or code-action resolve handler.                                                                                                                                                                                             |

The [server switch](../internal/lsp/server.go) advertises only document
sync, code actions and optional completion. Unsupported requests return LSP
`-32601`. The capabilities above are therefore a server checklist, not a claim
about what a particular editor may supply on its own.

## Inference and network boundary

The diagnostic path already infers `matrix`, `steps`, `needs`, `inputs`, `jobs`
and action/workflow outputs through
[`internal/checking/expressions`](../internal/checking/expressions). Local action
metadata, popular action metadata and reusable-workflow metadata enter its
[`Dependencies`](../internal/checking/expressions/checker.go). Unknown action
outputs remain an open string map in
[`dependency_types.go`](../internal/checking/expressions/dependency_types.go).
Current editor completion instead constructs a fresh expression checker from
the buffer. Thus diagnostic inference does **not** imply equivalent completion,
hover or navigation.

The native server accepts an injected
[`resolver.Resolver`](../internal/input/references/resolver.go) through
[`checking.Check`](../internal/checking/session.go). Resolvers implementing
`resolver.SessionFactory` create a fresh session for each analysis. They resolve
references for diagnostics and pin proposals. Separately, `--action-metadata`
selects the [remote action metadata service](remote-actions.md), which supplies
available action input and output declarations to diagnostic checks. Its default
is `off`; `offline` reads cached manifests and `online` can acquire them from
GitHub. This metadata does not supply editor completion or navigation targets.

The server does not yet meet the requested default: remote metadata typing must be
enabled by default with an explicit opt-out, reliable caching and permissions, per
[#157](https://github.com/kjanat/actionlint/issues/157#issuecomment-6353123385).
The provider and LSP integration is tracked in
[#336](https://github.com/kjanat/actionlint/issues/336). Explicit offline mode must
prevent network requests; missing cached metadata and provider failures must be
reported rather than treated as successful validation. An injected resolver alone
does not enable metadata acquisition.

Plugin and engine selection is held by the
[`editorSession`](../internal/lsp/plugin_session.go): explicit plugins and
remote engine URLs are startup snapshots; configured local artifacts are
identified by their contents. Plugins can return diagnostics and fixes; they
cannot register completion, hover or navigation handlers.

For the concrete extension path and acceptance checks, see the
[langserver implementation notes](../internal/lsp/README.md).
