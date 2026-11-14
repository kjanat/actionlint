# LSP implementation notes

The [readiness checklist](../../docs/lsp.md) records advertised behavior.
This package reads and writes JSON-RPC messages, tracks document versions,
converts UTF-16 positions, publishes diagnostics, and handles completion and
code-action requests. Workflow parsing, network resolution and plugin loading
are implemented elsewhere.

## Current data path

1. [`Serve`](server.go) overlays open buffers, schedules analysis of every open
   entry and cancels superseded generations. Closed dependencies can still
   receive diagnostics. A later publication clears removed diagnostics.
2. [`LanguageServer`](checks.go) opens an editor session,
   checks files with open-buffer contents taking precedence over disk, then adds
   the captured source revisions and diagnostics to [`Analysis`](server.go). It selects native and
   plugin checks through the same configuration as the CLI.
3. [`completion.Complete`](completion/completion.go) separately parses the
   current buffer. Its [expression completion helper](completion/expression.go)
   uses the expression checker's available contexts and property types.
   It knows the event and dispatch inputs but does not receive the job-specific
   types inferred while checking diagnostics. Completion therefore cannot yet
   offer the same properties that diagnostic checks validate.
4. [`codeActions`](actions.go) accepts only revisions matching published/open
   sources and emits versioned workspace edits. The client applies them.

## Requirements for further features

These are implementation requirements, not advertised features:

- [ ] Collect one immutable analysis snapshot for each generation. Include the
      captured source revisions, declarations, identifier and reference locations,
      inferred types, metadata origins and dependencies. Identify the source
      revision for every feature result. Share this snapshot across diagnostics and requests;
      avoid a second parser or per-request network resolver in the transport.
- [ ] Feed expression completion from the same context as diagnostics at the
      cursor: workflow/job/step scope, matrix, `needs`, step outputs, dispatch and
      reusable-call inputs. Keep open-buffer edits and UTF-16 conversion. Add
      focused tests that compare completion types with diagnostics on one buffer.
- [ ] Add hover, definition/declaration, references and document highlights using
      locations in captured sources. Define scope and targets
      explicitly: jobs, steps, outputs, local actions and local reusable workflows
      first. Return no target when metadata has no captured source location.
- [ ] Convert existing document outlines to document symbols. Add a workspace
      index and invalidation before workspace symbols. Build folding/selection
      ranges from the parser's YAML and expression spans, then semantic tokens with a declared
      legend and versioned refresh. A tree/dependency view needs a separately
      specified client protocol.
- [ ] Extend code actions only with revision-safe fixes from the existing fix
      model. Distinguish quick fixes, refactors and source-wide actions in
      capability advertisement and tests. Never turn a resolver suggestion into
      an unvalidated edit.
- [ ] Enable remote metadata typing by default, with explicit opt-out,
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
      failure must not silently disable validation. The native server currently
      passes the `RemoteActions` provider to checking and fetches metadata by default.
      Its `offline` and `online` modes
      supply available action declarations to diagnostic checks. They do not yet
      provide the editor features or complete reusable-workflow metadata described
      here. [`references.Resolver`](../input/references/resolver.go) resolves repository
      references; configuring it alone does not enable metadata acquisition.
- [ ] Define whether plugin metadata can add editor facts. Existing plugin
      analyzers supply findings and fixes, which do not register LSP feature
      handlers. If the interface is extended, version the additional typed data,
      identify its captured source, and validate it through the same path as
      native analysis.

[actup's LSP backend](https://github.com/kjanat/actup/blob/master/crates/lsp/src/backend.rs) advertises
`uses:` hover, completion, quick-fix/refactor/fix-all actions and workspace
commands; [hover.rs](https://github.com/kjanat/actup/blob/master/crates/lsp/src/hover.rs)
and [completion.rs](https://github.com/kjanat/actup/blob/master/crates/lsp/src/completion.rs) implement those
requests. Its Zed extension supplies a tree-sitter grammar and highlight,
injection and outline queries. Those editor queries do not provide LSP
folding, semantic tokens, symbols or navigation here.
