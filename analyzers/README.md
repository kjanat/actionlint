# Structural architecture checks

The analyzer consumes Go type information and SSA. There is no architecture
manifest, package-layer assignment, protected-type registry or retired-field
list. It also works in modules unrelated to Actionlint.

| Code   | Rule                                                                                                                                                                         |
| ------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ARC001 | Public API signatures, aliases, fields and constraints cannot expose types from an `internal` package. Implementation imports and private representation fields are allowed. |
| ARC002 | An exported struct with private representation or receiver-mutating methods cannot expose writable fields. Plain value records without either property are allowed.          |
| ARC003 | Exported methods cannot return mutable storage reachable from their receiver. Return detached storage or an opaque value handle.                                             |
| ARC004 | Calls without source summaries may mutate receiver storage or return aliases. Unresolved ownership and detachment fail explicitly.                                           |

ARC003 follows assignments, branches, field/index access, pointer loads,
interfaces, closures, containers and function results. Parameter/result and
mutation summaries propagate through local recursion and imported source helpers.
Returning an alias through another package does not establish ownership.

`bytes.Clone` and flat `slices.Clone`/`maps.Clone` results allocate detached
storage. Shallow clones containing exposed references still share storage.
Fresh byte copies using `make`/`copy` or `append` are also allowed. Value handles
with only private representation can share their internal storage; their public
methods are checked separately. A constructor returning fresh storage is allowed.

A returned closure, such as an `iter.Seq` iterator, carries a captured variable
only when its body can expose it: by returning it with a mutable type, by
writing it, by storing it into a parameter, or by passing it with a mutable type
to code without a summary, such as the iterator's `yield` function. An iterator
that only reads its receiver and yields pointer-free values or read-only value
handles is not reported. An iterator that yields receiver slices, maps or
pointers to receiver fields is, as is a closure that writes receiver state.
A method value, such as `w.store.Row`, carries its bound receiver as a capture,
checked against the method's summary like any other closure.

A closure passed to a call may be invoked by the callee, as a range-over-func
iterator invokes its loop body. Summaries do not record what a callee passes to
such a closure, so the analyzer assumes it may pass storage reachable from the
call's other arguments, or from the values they were built from. This holds
for unknown iterators, local closure literals, functions in this package and
imported method values alike. A body that stores what it receives into a
captured variable makes that variable alias that storage, with an uncertain
origin; storing another captured variable carries that variable's origins. A
body that writes the values it receives counts as a possible write to that
storage. The same applies to a closure literal called through a variable that
may hold one of several literals.

A value receiver whose type holds no storage, such as `struct{}`, is a private
copy and has no receiver origin. A variable joining fresh allocations, such as
`maps.Clone` on one branch and a map literal on the other, is fresh storage:
writing it changes no parameter.

These checks enforce structural properties.
Alias findings are conservative: complex deep copies through loops can need
review even when the resulting value is detached. ARC003 identifies a possible
receiver-to-result storage path; it is not a runtime proof of mutation.
They cannot establish whether two differently represented algorithms implement
the same responsibility. They do not infer which packages should perform I/O,
prove concurrency safety or verify Go/WIT equivalence. Unknown/interface calls
produce ARC004 for unresolved alias and mutation effects. This includes standard-library
calls whose bodies are unavailable, even when they only read their arguments.
Calls through a function value are unknown calls too, including a closure
returned by a summarized function: what such an iterator hands its loop body is
not summarized. Passing a read-only value handle to an unknown call is not a
possible write. The analyzer assumes that code changes a handle only through its
exported methods, so an interface implementation in the same package that
writes a handle's private fields is missed. So is code in the same package that
writes the private fields of handles collected by `append`, a clone or an
unknown call, or of handles a helper function receives through an iterator
parameter. A write into a container whose freshness cannot be proven, such as a
slice element, also counts the values stored into that container as written.
Aggregate value copies use
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
