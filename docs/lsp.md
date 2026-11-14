# Language server readiness

Status: source audit of this checkout, 2026-10-27. **Implemented** means the
`actionlint lsp` server advertises and handles the feature. **Partial** means
useful code or editor support exists, but the stated LSP feature is incomplete.
Editor grammar features are separate from server capabilities.

Run `actionlint lsp` with the editor connected to stdin and stdout. LSP positions
use UTF-16 code units. The server tracks document versions and applies incremental
edits to open documents. It analyzes captured source revisions and does not publish
results from cancelled or superseded analyses. See [transport](../internal/lsp/server.go),
[editor integration](../internal/lsp/checks.go), and
[usage](usage.md#editor-fixes-and-remote-resolution).

| Feature                          | Status      | Evidence and missing work                                                                                                                                                                                                                                                                                                                                                                                                                    |
| -------------------------------- | ----------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Syntax highlighting              | External    | The server advertises no color provider or semantic tokens. An editor grammar can highlight YAML and expressions; this repository supplies no such grammar.                                                                                                                                                                                                                                                                                  |
| Folding                          | Absent      | The server has no `textDocument/foldingRange` handler. The YAML parser builds syntax nodes, but the server does not return folding ranges.                                                                                                                                                                                                                                                                                                   |
| Syntax tree                      | Partial     | The YAML parser and workflow declarations exist, but the server has no tree view or custom tree request. A tree view needs a request and response format that both server and editor understand.                                                                                                                                                                                                                                             |
| Selection ranges                 | Absent      | The server has no `textDocument/selectionRange` handler. It would need ranges for nested YAML and expressions in the captured text.                                                                                                                                                                                                                                                                                                          |
| Diagnostics                      | Implemented | [`Serve`](../internal/lsp/server.go) publishes findings for open documents and files they depend on, such as local actions. [Diagnostic conversion](../internal/lsp/diagnostics.go) adds severity, code, links, and related locations. Results from superseded analyses are discarded.                                                                                                                                                       |
| Add diagnostic suppressions      | Implemented | Quick fixes insert or extend YAML suppression comments in open buffers. They keep any reason and target the block header or declaration that produced the finding. The rule must allow suppression, and the open document must still have the revision used for analysis.                                                                                                                                                                    |
| Semantic tokens                  | Absent      | The server has no token legend, full or range token handlers, or token invalidation. These require tokenizing the parsed YAML and expressions before the server advertises support.                                                                                                                                                                                                                                                          |
| Completion                       | Partial     | [Editor completion](../internal/lsp/completion/completion.go) offers expression roots and properties, event payload fields, and `workflow_dispatch` inputs found in the open buffer. [Transport](../internal/lsp/completion.go) converts edit positions to UTF-16. Completion does not use the checker's job, matrix, step, reusable-workflow, or action metadata types. It also does not complete YAML keys, values, or `uses:` references. |
| Definition / declaration         | Absent      | The server has no navigation handlers. Navigation needs source locations for local action and workflow references and workflow identifiers. A remote target also needs source text that the editor can open.                                                                                                                                                                                                                                 |
| Hover                            | Absent      | The server has no `textDocument/hover` handler. It does not return expression types or rule descriptions for the cursor position. A hover handler must use captured source positions and must not fetch data from the network while answering a request.                                                                                                                                                                                     |
| Document symbols                 | Partial     | Analysis returns [document outlines](design/analysis-contracts.md), but the server has no `textDocument/documentSymbol` handler to send them as editor symbols.                                                                                                                                                                                                                                                                              |
| Dependency tree                  | Partial     | Analysis tracks dependencies for checks and to invalidate diagnostics when a dependency changes. The server has no request that returns those dependencies as a tree.                                                                                                                                                                                                                                                                        |
| References / document highlights | Absent      | The server has no `textDocument/references` or `textDocument/documentHighlight` handlers. These features need an index that connects identifiers to their references and captured source locations.                                                                                                                                                                                                                                          |
| Workspace symbols                | Absent      | The server has no `workspace/symbol` handler or workspace document index. Open buffers alone do not provide symbols from the rest of the workspace.                                                                                                                                                                                                                                                                                          |
| Assists / code actions           | Partial     | [Quick fixes](../internal/lsp/actions.go) return diagnostic edits as versioned `documentChanges`. The server has no refactors, fix-all, action-reference update or pin actions, or code-action resolve handler.                                                                                                                                                                                                                              |

The [server switch](../internal/lsp/server.go) advertises document sync and code
actions, plus completion when enabled. It does not advertise the other features
in the table, and returns LSP error `-32601` for their requests. An editor may
still provide its own grammar features, such as syntax highlighting.

## Expression types and remote metadata

The checker uses workflow structure and available action or reusable-workflow
metadata to type `matrix`, `steps`, `needs`, `inputs`, `jobs`, and output
expressions. It receives metadata for actions and reusable workflows through its
[`Dependencies`](../internal/checking/expressions/checker.go). When action metadata
is unavailable, the checker cannot validate output names against a declaration. Editor completion builds a separate
expression checker from the open buffer, so it does not receive the checker's
inferred job, matrix, step, or metadata types. Diagnostics can therefore use facts
that completion, hover, and navigation do not yet provide.

The native server can receive a [`resolver.Resolver`](../internal/input/references/resolver.go)
through [`checking.Check`](../internal/checking/session.go). A resolver that
implements `resolver.SessionFactory` gets a new session for each analysis. The
resolver supports reference diagnostics and pin proposals. CLI checks use
`--online` by default; `--offline` reads cached manifests without fetching missing
dependencies. The checker
can use acquired action inputs and outputs in diagnostics, but this metadata does
not supply editor completion or navigation targets.

Issue [#157](https://github.com/kjanat/actionlint/issues/157#issuecomment-6353123385)
requests metadata-backed expression typing by default, an opt-out, and defined
caching and permission behavior. Issue [#336](https://github.com/kjanat/actionlint/issues/336)
tracks the provider and LSP integration. Offline mode must make no network requests;
when the cache lacks a manifest or a provider fails, analysis must report that
metadata was unavailable. Supplying a resolver alone does not acquire manifests.

Plugin and engine selection is held by the
[`editorSession`](../internal/lsp/plugin_session.go): explicit plugins and
remote engine URLs are startup snapshots; configured local artifacts are
identified by their contents. Plugins can return diagnostics and fixes; they
cannot register completion, hover or navigation handlers.

For the concrete extension path and acceptance checks, see the
[langserver implementation notes](../internal/lsp/README.md).
