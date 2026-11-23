# Language server readiness

Run `actionlint lsp` and connect the editor to its stdin and stdout.

| Feature                 | Status      | Notes                                                                                                                                                                                                       |
| ----------------------- | ----------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Diagnostics             | Implemented | The server publishes findings for open documents and their dependencies, such as local actions. Diagnostics carry severity, code, links and related locations.                                              |
| Suppression quick fixes | Implemented | A quick fix inserts or extends a YAML suppression comment and keeps its reason. The rule must allow suppression, and the analysis must cover the current revision of the open document.                     |
| Code actions            | Partial     | Quick fixes return diagnostic edits as versioned `documentChanges`. The server has no refactors, fix-all or code-action resolve handler.                                                                    |
| Completion              | Partial     | Completion offers expression roots and properties, event payload fields and `workflow_dispatch` inputs from the open buffer. It ignores job, matrix, step and metadata types, YAML keys and `uses:` values. |

`actionlint lsp` accepts the network, plugin and tool options of `actionlint check`. It fetches remote action metadata
for diagnostics unless you pass `--offline`. Plugins add diagnostics and fixes but no other editor features. Restart the server after installing,
updating or removing plugins.
