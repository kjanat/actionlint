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
`context.Context.Err` only reads its context and returns nil,
`context.Canceled` or `context.DeadlineExceeded`, as the Context contract
documents, so a stored context's error aliases nothing. Its other methods have
no model. Locking or unlocking a `sync.Mutex` or `sync.RWMutex` changes only
the lock, so a method that locks only to read its storage stays read-only.
Whether locks are balanced is not checked.

`maps.Keys`, `maps.Values`, `maps.All`, `slices.All` and `slices.Values` write
nothing and return an iterator that only reads its argument. The iterator
carries the argument's storage only when it yields storage, such as pointer
values; one yielding string keys exposes none. `slices.Sorted` and
`slices.Collect` applied directly to such an iterator write nothing and return
a new slice holding what it yields.

Summaries record what a function passes to its function-typed parameters, such
as an iterator's `yield`, as long as it only calls them. For a returned
function value, such as an `iter.Seq`, they record what that function will
pass when called. A range loop over an iterator from a closure literal, a
source function, a summarized call or one of the iterators above hands the
loop body only what the iterator yields. An iterator that writes its receiver
to report diagnostics but yields fresh values therefore does not make the loop
body alias the receiver; `slices.Collect` and `slices.Sorted` over it return a
new slice of those values. When a `yield` parameter is stored or passed on,
what it receives is unknown and the loop falls back to the rule below.

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
iterator invokes its loop body. Except for the range loops and collections
above, the analyzer does not use what a callee passes to such a closure. It
assumes the callee may pass storage reachable from the call's other arguments,
or from the values they were built from. This holds for unknown iterators,
iterators that pass their `yield` on, a closure literal called directly,
functions in this package called with a closure argument and imported method
values alike. A body that stores what it receives into a
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
returned by a summarized function: its writes and results are not summarized,
only what it hands its loop body. Passing a read-only value handle to an unknown call is not a
possible write. The analyzer assumes that code changes a handle only through its
exported methods, so an interface implementation in the same package that
writes a handle's private fields is missed. So is code in the same package that
writes the private fields of handles collected by `append`, a clone, an
assignment to a slice element or an unknown call, or of handles a helper function receives through an iterator
parameter. A write into a container whose freshness cannot be proven, such as a
slice element, also counts the values stored into that container as written.
Aggregate value copies use
dominating field stores to recognize detachment before publication. A local
struct copied from another local, such as `w := T{m: map[K]V{}}`, keeps the
fields stored into that local, so writing `w.m` writes no parameter. Other flows
are flow-insensitive. The analyzer does not implement a whole-program heap
analysis; reflection and arbitrary unsafe
memory operations need separate review. Standard-library bodies are not analyzed;
the clone, iterator, `context.Context.Err` and mutex lock calls above have
explicit models. Diagnostics concern production declarations; test code still
participates in helper summaries.

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
