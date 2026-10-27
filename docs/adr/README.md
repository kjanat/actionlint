# Architecture decision records

Format per file: Status · Context · Decision · Consequences · Open.
Numbered, never renumbered. Superseded ADRs stay and point forward.

| #                                        | title                                                             | status   |
| ---------------------------------------- | ----------------------------------------------------------------- | -------- |
| [0001](0001-rust-blank-slate.md)         | Rust, blank slate, no behavioural port                            | accepted |
| [0002](0002-tree-sitter-syntax.md)       | tree-sitter-github-actions is the only syntax source              | accepted |
| [0003](0003-immutable-revisions.md)      | Immutable documents keyed by `(DocumentId, Revision)`             | accepted |
| [0004](0004-query-engine-salsa.md)       | Demand-driven query engine on salsa                               | accepted |
| [0005](0005-engine-is-domain-free.md)    | The engine knows no document kind, tool, or YAML                  | accepted |
| [0006](0006-invocation-identity.md)      | Occurrence = call-site path, `uses:` resolution is cache-only     | accepted |
| [0007](0007-diagnostics-and-fixes.md)    | Diagnostics carry provenance, fixes are revision-pinned edits     | accepted |
| [0008](0008-no-tool-specific-code.md)    | No external-tool integration in this repository                   | accepted |
| [0009](0009-extension-contract.md)       | Extensions: WIT components, capability-negotiated, pull-only      | accepted |
| [0010](0010-config-schema.md)            | Config via serde + schemars, schema generated and diff-checked    | accepted |
| [0011](0011-cli-semantics.md)            | CLI exit codes and bare-invocation behaviour                      | accepted |
| [0012](0012-generated-code-policy.md)    | Generated code is not committed, except the config schema         | accepted |
| [0013](0013-lsp-transport.md)            | LSP transport: `lsp-server` + `gen-lsp-types`, synchronous loop   | accepted |
| [0014](0014-source-of-truth.md)          | Source of truth is GitHub's runner schema and template semantics  | accepted |
| [0015](0015-rule-taxonomy-and-config.md) | Rule ids are `area/name`, category is metadata, one `[rules]` map | accepted |
