# Structural architecture checks

The analyzer consumes Go type information and SSA. There is no architecture
manifest, package-layer assignment, protected-type registry or retired-field
list. It also works in modules unrelated to Actionlint.

| Code   | Rule                                                                                                                                                                         |
| ------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ARC001 | Public API signatures, aliases, fields and constraints cannot expose types from an `internal` package. Implementation imports and private representation fields are allowed. |
| ARC002 | An exported struct with private representation or receiver-mutating methods cannot expose writable fields. Plain value records without either property are allowed.          |
| ARC003 | Exported methods cannot return mutable storage reachable from their receiver. Return detached storage or an opaque value handle.                                             |
| ARC004 | A reference-returning call without a source summary prevents establishing detachment from the receiver. The unresolved flow fails explicitly.                                |

ARC003 follows assignments, branches, field/index access, pointer loads,
interfaces, closures, containers and function results. Parameter/result and
mutation summaries propagate through local recursion and imported source helpers.
Returning an alias through another package does not establish ownership.

`bytes.Clone` and flat `slices.Clone`/`maps.Clone` results allocate detached
storage. Shallow clones containing exposed references still share storage.
Fresh byte copies using `make`/`copy` or `append` are also allowed. Value handles
with only private representation can share their internal storage; their public
methods are checked separately. A constructor returning fresh storage is allowed.

These checks enforce structural properties, not all semantic architecture.
Alias findings are conservative: complex deep copies through loops can need
review even when the resulting value is detached. ARC003 identifies a possible
receiver-to-result storage path; it is not a runtime proof of mutation.
They cannot establish whether two differently represented algorithms implement
the same responsibility. They do not infer which packages should perform I/O,
prove concurrency safety or verify Go/WIT equivalence. Unknown/interface calls
produce ARC004 rather than a claim of a proven alias. Aggregate value copies use
dominating field stores to recognize detachment before publication. Other flows
are flow-insensitive. The analyzer does not implement a whole-program heap
analysis; reflection and arbitrary unsafe
memory operations need separate review. Standard-library bodies are not analyzed;
the clone operations above have explicit models. Diagnostics concern production
declarations; test code still participates in helper summaries.

The default golangci-lint configuration additionally enables `dupl` for structural
code duplication, `contextcheck` for propagation and `exhaustive` for enum switches.
No diagnostics baseline or project-specific suppression is installed.

`make lint-build` builds both runners using golangci-lint 2.15.0 and clears its
result cache after the plugin build. `make test-analyzers` tests the analyzer.
`make lint-architecture` runs the standalone checker without golangci exclusions;
`make lint` runs both checks. The Architecture workflow tests the custom binary in
an unrelated module, including a source-only fix under a warm cache, then runs
product enforcement. Zed uses the built `custom-gcl` executable.

Product enforcement is expected to fail until the reported ownership and API
violations are fixed. Analyzer test success does not certify the whole core.
