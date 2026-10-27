# Language server

Run `actionlint lsp --plugin path/to/component.wasm` for stdio LSP.
Use `actionlint lsp --config actionlint.json` to load the shared
[application configuration](../actionlint-app/README.md).
The server analyzes unsaved buffers through the same application and component
engine as `check`. One background worker analyzes captured revisions. Pending changes coalesce per
document; obsolete results are discarded before publication.

## Features

- [x] Initialize, shutdown and exit
- [x] Open, incremental change and close notifications
- [x] UTF-16 positions
- [x] Versioned diagnostic reporting and related locations
- [x] Clear diagnostics on document close
- [x] Explicit warnings for incomplete analysis
- [x] Background analysis and stale-result rejection
- [ ] Interrupt an in-flight analysis on cancellation
- [ ] Syntax highlighting
- [ ] Folding and selection ranges
- [ ] Syntax tree inspection
- [ ] Semantic highlighting
- [ ] Completion and hover
- [ ] Definitions, declarations and references
- [ ] Document highlights and symbols
- [ ] Workspace symbols and dependency tree
- [x] Versioned quickfix proposals from analyzer diagnostics
- [ ] Formatting
- [ ] Remote input/output type inference

Only implemented protocol capabilities are advertised. The grammar and rule
coverage are still incomplete; a successful request is not full workflow coverage.

`tests/protocol.rs` exercises initialization, unsaved text, incremental changes,
stale-version rejection, close and shutdown with a real component over an
in-memory LSP connection. Build the component with `cargo run -p xtask --
component-conformance` before running this test independently.

Shutdown stops scheduling work and waits for the current analysis. In-flight
component execution is fuel-bounded; immediate cancellation is not implemented.

Quickfixes target the captured open document version. The server does not apply
workspace edits itself. Multi-document fixes need captured versions for every
target and are not yet exposed as code actions.
Quickfixes are advertised and served only when the client supports code-action
literals and versioned document changes. Other clients still receive diagnostics.
