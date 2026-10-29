# LSP implementation notes

The [readiness checklist](../../docs/lsp.md) records advertised behavior.
This package owns JSON-RPC framing, document versions, UTF-16 translation,
diagnostic publication, completion transport and code-action transport. It
does not own workflow parsing, network resolution or plugin loading.

## Current data path

1. [`Serve`](server.go) overlays open buffers, schedules analysis of every open
   entry and cancels superseded generations. Closed dependencies can still
   receive diagnostics. A later publication clears removed diagnostics.
2. [`localapp.LanguageServer`](../localapp/lsp.go) opens an editor session,
   calls `application.Check` with source overlays, then admits captured source
   revisions and diagnostics into [`Analysis`](server.go). It keeps native and
   plugin selection consistent with normal checks.
3. [`editor.Complete`](../workflow/editor/completion.go) separately parses the
   current buffer and calls [`expression.Checker.Complete`](../expression/completion.go).
   It has event and dispatch-input context, not the per-job types built during
   the diagnostic traversal. That split is the immediate completion gap.
4. [`codeActions`](actions.go) accepts only revisions matching published/open
   sources and emits versioned workspace edits. The client applies them.

## Extension contract to implement

These are implementation requirements, not advertised features:

- [ ] Project one immutable editor analysis snapshot per generation: captured
      source revisions, declarations, identifier/ref locations, inferred types,
      metadata provenance and dependency edges. Bind every feature result to its
      source revision. Share this projection across diagnostics and requests;
      avoid a second parser or per-request network resolver in the transport.
- [ ] Feed expression completion from the same context as diagnostics at the
      cursor: workflow/job/step scope, matrix, `needs`, step outputs, dispatch and
      reusable-call inputs. Keep open-buffer edits and UTF-16 conversion. Add
      focused tests that compare completion types with diagnostics on one buffer.
- [ ] Add source-backed hover, definition/declaration, references and document
      highlights for each supported identifier/ref kind. Define scope and targets
      explicitly: jobs, steps, outputs, local actions and local reusable workflows
      first. Return no target when metadata has no captured source location.
- [ ] Project existing document outlines to document symbols. Add a workspace
      index and invalidation before workspace symbols. Build folding/selection
      ranges from owned YAML/expression spans, then semantic tokens with a declared
      legend and versioned refresh. A tree/dependency view needs a separately
      specified client protocol; memory layout has no workflow meaning.
- [ ] Extend code actions only with revision-safe fixes from the existing fix
      model. Distinguish quick fixes, refactors and source-wide actions in
      capability advertisement and tests. Never turn a resolver suggestion into
      an unvalidated edit.
- [ ] Implement remote metadata typing enabled by default, with explicit opt-out,
      as requested in [#157](https://github.com/kjanat/actionlint/issues/157#issuecomment-6353123385)
      and the actup integration workstream [#336](https://github.com/kjanat/actionlint/issues/336).
      The provider result must contain
      action/reusable-workflow input and output names, types, immutable ref identity,
      source provenance and failure state. Thread it through application analysis
      and the existing expression `Dependencies`; keep absent/dynamic schemas
      open rather than claim strict types. Invalidate on editor changes and provider
      identity changes; preserve cancellation. Specify reliable caching and
      permissions. Explicit offline mode must prevent network requests and report
      unavailable metadata when the cache cannot satisfy a request; provider
      failure must not silently disable validation. Current
      [`resolver.Resolver`](../../resolver/resolver.go) returns ref evidence, not
      these schemas, so this requires a new explicit contract.
- [ ] Define whether plugin metadata can add editor facts. Existing plugin
      analyzers supply findings/fixes; avoid inferring LSP providers from that ABI.
      If extended, require typed, versioned facts and source provenance through
      the same admission boundary as native analysis.

[actup's LSP backend](https://github.com/kjanat/actup/blob/master/crates/lsp/src/backend.rs) advertises
`uses:` hover, completion, quick-fix/refactor/fix-all actions and workspace
commands; [hover.rs](https://github.com/kjanat/actup/blob/master/crates/lsp/src/hover.rs)
and [completion.rs](https://github.com/kjanat/actup/blob/master/crates/lsp/src/completion.rs) implement those
requests. Its Zed extension supplies a tree-sitter grammar and highlight,
injection and outline queries. Those editor queries do not provide LSP
folding, semantic tokens, symbols or navigation here.
