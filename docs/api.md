# Integration interfaces

Use the command-line interface to check files, the language server to connect an
editor, or a plugin to add checks. The Go host implementation lives under
`internal/`; it is not an importable library API.

## Choose an interface

| Task                                             | Interface                                         | Documentation                                                   |
| ------------------------------------------------ | ------------------------------------------------- | --------------------------------------------------------------- |
| Check workflows and actions from another program | CLI with JSON, JSONL or SARIF output              | [Output formats](usage.md#output-and-machine-readable-metadata) |
| Show diagnostics and offer edits in an editor    | Language Server Protocol over stdio               | [Language server](lsp.md)                                       |
| Connect a browser editor                         | Browser worker and LSP client library             | [Browser LSP library](../libraries/browser-lsp/README.md)       |
| Write a plugin in TypeScript                     | Provider declarations, packaging and test helpers | [TypeScript SDK](../plugins/sdk/typescript/README.md)           |
| Write a component plugin in Rust                 | Guest bindings and export macros                  | [Rust SDK](../plugins/sdk/rust/README.md)                       |
| Write a component plugin in Go                   | Guest bindings for TinyGo                         | [Go SDK](../plugins/sdk/go/README.md)                           |
| Wrap an executable or container-based tool       | Process-provider manifest and output transform    | [Plugins](plugins.md)                                           |
| Resolve remote action and workflow references    | Resolver request/response protocol                | [Remote actions](remote-actions.md)                             |

## Check files from another program

Run actionlint as a child process and select a machine-readable output format:

```sh
actionlint --output json .github
```

Read stdout for the requested report and stderr for operational messages. Exit
codes distinguish a clean check (`0`), findings (`1`), invalid arguments (`2`) and
an incomplete check (`3`). An incomplete check can still report findings from
checks that completed. Inspect both the report and the exit code.

JSON contains the complete result and completion status. JSONL emits one
versioned diagnostic record per line; it does not encode completion status.
Use JSON when your program needs that status in the report itself.

Use `actionlint config schema` to obtain a configuration schema with the selected
plugins included. Use `actionlint rules` to list rules and their declarations.
These commands expose their data without requiring imports of host Go packages.

## Connect an editor

Run `actionlint lsp` with stdin and stdout connected to the client. Send and
receive LSP messages through that transport. The server uses the same diagnostic
checks as the CLI and offers completion and code actions through LSP.

For browser workers and VS Code web clients, use
[`@kjanat-actionlint/language-server`](../libraries/browser-lsp/README.md).
The package supplies the worker transport and compiled Go server. The
[playground](../applications/playground/README.md) uses that library.

The [language server feature table](lsp.md) describes implemented features and
features that are not available yet.

## Write and test a plugin

Declare the data your plugin needs through its interface. The host sends that
data to the plugin and processes its findings, suppressions and proposed edits
through the same code path as built-in checks.

- The [TypeScript SDK](../plugins/sdk/typescript/README.md) supplies typed provider
  declarations, packaging commands and fixture-test helpers. Its package root
  does not access files or start processes; native helpers have separate exports.
- The [Rust SDK](../plugins/sdk/rust/README.md) supplies component guest bindings
  and export macros.
- The [Go SDK](../plugins/sdk/go/README.md) supplies component guest bindings for
  TinyGo. It is a separate module and contains no host runtime implementation.

The [plugin examples](../plugins/examples) contain manifests, source,
build instructions and tests. Start with an example for the kind of plugin you
want to write.

Authoritative component interfaces live under
[`plugins/protocol/wit`](../plugins/protocol/wit). JSON metadata and message
schemas live under [`plugins/protocol/json`](../plugins/protocol/json). SDK and
host bindings derive from those definitions.

Positions and edits follow the coordinate convention of the selected interface.
Analyzer component positions and TypeScript SDK fixes use one-based Unicode
character columns with exclusive range ends. A process-provider manifest can
declare the tool's row, column, and end conventions; when omitted, the host uses
LF rows, Unicode character columns, and exclusive ends.

Revision requirements also depend on the interface. Analyzer component fixes
include a digest for each edited document. TypeScript fixes have no revision
field, so the host binds an edit to the current source only when it targets the
document that produced the finding. Before applying fixes, actionlint checks the
recorded revisions and ranges against current files. The language server also
ties quick fixes to the open document version. Do not reuse findings after source
changes. Fixes are optional, and some need review before application.

## Host implementation and registry validation

The CLI and language server call the private checking code. To connect another
program, use the CLI, LSP or plugin interfaces described above.

The registry has its own unpublished adapter module for testing plugins against
an exact host revision. That adapter's imports do not make the host packages part
of the plugin SDK or a public Go library.
