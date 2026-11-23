# LSP implementation notes

The [readiness checklist](../../docs/lsp.md) records advertised behavior.

## Current data path

1. [`Serve`](server.go) overlays open buffers, schedules analysis of every open entry and cancels superseded generations.
2. [`LanguageServer`](checks.go) prefers open-buffer contents over disk and records source revisions and diagnostics in [`Analysis`](server.go). It selects checks through the same configuration as the CLI.
3. [`completion.Complete`](completion/completion.go) parses the current buffer separately. Its [expression helper](completion/expression.go) knows the event and dispatch inputs. It lacks the job-specific types that diagnostics infer.
4. [`codeActions`](actions.go) accepts only revisions that match published or open sources and emits versioned workspace edits.
