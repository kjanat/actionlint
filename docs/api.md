# Integration interfaces

## Choose an interface

| Task                                    | Interface                            | Documentation                                                                                                               |
| --------------------------------------- | ------------------------------------ | --------------------------------------------------------------------------------------------------------------------------- |
| Check files from another program        | CLI with JSON, JSONL or SARIF output | [Output formats](usage.md#output-and-machine-readable-metadata)                                                             |
| Show diagnostics and edits in an editor | `actionlint lsp` over stdio          | [Language server](lsp.md)                                                                                                   |
| Connect a browser editor                | Browser worker and LSP client        | [Browser LSP library](../libraries/browser-lsp/README.md)                                                                   |
| Write a plugin                          | TypeScript, Rust or TinyGo SDK       | [TypeScript](../plugins/sdk/typescript/README.md), [Rust](../plugins/sdk/rust/README.md), [Go](../plugins/sdk/go/README.md) |
| Wrap an executable or container         | Process-provider manifest            | [Plugins](plugins.md)                                                                                                       |
| Resolve remote references               | Resolver protocol                    | [Remote actions](remote-actions.md)                                                                                         |
